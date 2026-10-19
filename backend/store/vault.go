package store

import (
	"database/sql"
	"errors"
)

// El llavero propio de ScadLab (S12, 12.10) guarda en la base solo lo mínimo, y
// nada de eso abre algo por sí solo:
//
//	vault_meta.verifier(+nonce)  un texto conocido cifrado con la llave derivada
//	                             de la contraseña maestra (argon2id). Abrirlo
//	                            prueba que la contraseña es la buena.
//	vault_meta.remember          si la llave derivada está en el llavero del SO.
//	model_servers.encrypted_token las claves de API, AES-256-GCM con esa llave.
//
// Este paquete no deriva ni cifra: recibe y devuelve bytes opacos (la criptografía
// vive en backend/vaultgate). Lo que SÍ hace es lo que tiene que ser atómico —
// cambiar el verificador y re-cifrar todas las claves van en UNA transacción:
// si se corta a la mitad, las claves siguen abriendo con la contraseña vieja y
// nunca quedan unas con cada llave.

// VaultMeta es la fila única de vault_meta.
type VaultMeta struct {
	Verifier  []byte
	Nonce     []byte
	Remember  bool
	CreatedAt int64
}

// Reencrypt convierte una clave cifrada en otra. Devolver (nil, nil, nil) borra
// la clave guardada (no se pudo abrir y no hay forma de recuperarla); un error
// cancela toda la operación.
type Reencrypt func(cipher, nonce []byte) (newCipher, newNonce []byte, err error)

// GetVaultMeta lee la fila; ok=false si todavía no se creó la contraseña maestra.
func (s *Store) GetVaultMeta() (m VaultMeta, ok bool, err error) {
	var remember int
	err = s.db.QueryRow(`SELECT verifier, verifier_nonce, remember, created_at FROM vault_meta WHERE id = 1`).
		Scan(&m.Verifier, &m.Nonce, &remember, &m.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return VaultMeta{}, false, nil
	}
	if err != nil {
		return VaultMeta{}, false, err
	}
	m.Remember = remember != 0
	return m, true, nil
}

// WriteVault guarda el verificador (alta o cambio de contraseña) y re-cifra con
// re cada clave de API guardada, todo en una transacción. re puede ser nil si no
// hay nada que re-cifrar. Conserva created_at si la fila ya existía.
func (s *Store) WriteVault(m VaultMeta, re Reencrypt) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op tras Commit

	if re != nil {
		rows, err := tx.Query(`SELECT id, encrypted_token, token_nonce FROM model_servers WHERE encrypted_token IS NOT NULL`)
		if err != nil {
			return err
		}
		type tok struct {
			id            string
			cipher, nonce []byte
		}
		var toks []tok
		for rows.Next() {
			var t tok
			if err := rows.Scan(&t.id, &t.cipher, &t.nonce); err != nil {
				rows.Close()
				return err
			}
			toks = append(toks, t)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for _, t := range toks {
			c, n, err := re(t.cipher, t.nonce)
			if err != nil {
				return err
			}
			if c == nil {
				_, err = tx.Exec(`UPDATE model_servers SET encrypted_token = NULL, token_nonce = NULL WHERE id = ?`, t.id)
			} else {
				_, err = tx.Exec(`UPDATE model_servers SET encrypted_token = ?, token_nonce = ? WHERE id = ?`, c, n, t.id)
			}
			if err != nil {
				return err
			}
		}
	}

	remember := 0
	if m.Remember {
		remember = 1
	}
	if _, err := tx.Exec(`INSERT INTO vault_meta (id, verifier, verifier_nonce, remember, created_at) VALUES (1, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET verifier = excluded.verifier, verifier_nonce = excluded.verifier_nonce, remember = excluded.remember`,
		m.Verifier, m.Nonce, remember, now()); err != nil {
		return err
	}
	return tx.Commit()
}

// SetVaultRemember cambia solo la marca «Recordar en este equipo». No hace nada
// si todavía no hay contraseña maestra.
func (s *Store) SetVaultRemember(remember bool) error {
	v := 0
	if remember {
		v = 1
	}
	_, err := s.db.Exec(`UPDATE vault_meta SET remember = ? WHERE id = 1`, v)
	return err
}

// ResetVault borra el verificador y TODAS las claves de API guardadas, en una
// transacción, y devuelve cuántas claves se borraron. Los servidores quedan
// (nombre, dirección, variable de entorno): solo pierden su clave.
func (s *Store) ResetVault() (cleared int, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck // no-op tras Commit
	res, err := tx.Exec(`UPDATE model_servers SET encrypted_token = NULL, token_nonce = NULL WHERE encrypted_token IS NOT NULL`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if _, err := tx.Exec(`DELETE FROM vault_meta`); err != nil {
		return 0, err
	}
	return int(n), tx.Commit()
}

// CountModelServerTokens cuenta los servidores con una clave guardada.
func (s *Store) CountModelServerTokens() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM model_servers WHERE encrypted_token IS NOT NULL`).Scan(&n)
	return n, err
}

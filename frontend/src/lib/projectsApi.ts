import type {main} from '../../wailsjs/go/models'
import {
    CloseProject,
    ListOpenProjects,
    OpenProject,
    ProjectActivity,
    ReorderProjects,
    SetActiveProject,
} from '../../wailsjs/go/main/App'

// Proyectos abiertos (S8, backend: app_projects.go). Lo único que añade este
// módulo a los bindings es MAX_PROJECTS y los nombres de los tipos que usa la UI.

/** Máximo de proyectos abiertos a la vez (el backend rechaza el quinto: store.MaxOpenProjects). */
export const MAX_PROJECTS = 4

export type OpenProjectInfo = main.OpenProjectEntry
export type OpenProjectsList = main.OpenProjectsView
export type ProjectActivityItem = main.ProjectActivityEntry

export const listOpenProjects = ListOpenProjects
export const openProject = OpenProject
export const closeProject = CloseProject
export const setActiveProject = SetActiveProject
export const reorderProjects = ReorderProjects
export const projectActivity = ProjectActivity

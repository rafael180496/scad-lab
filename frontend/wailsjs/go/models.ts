export namespace agentsessions {
	
	export class Session {
	    id: string;
	    title: string;
	    updatedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new Session(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.updatedAt = source["updatedAt"];
	    }
	}

}

export namespace checkpoint {
	
	export class TrashedItem {
	    name: string;
	    original: string;
	    parent: string;
	    description: string;
	    trashedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new TrashedItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.original = source["original"];
	        this.parent = source["parent"];
	        this.description = source["description"];
	        this.trashedAt = source["trashedAt"];
	    }
	}

}

export namespace detect {
	
	export class Tool {
	    id: string;
	    found: boolean;
	    path: string;
	    version: string;
	
	    static createFrom(source: any = {}) {
	        return new Tool(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.found = source["found"];
	        this.path = source["path"];
	        this.version = source["version"];
	    }
	}

}

export namespace estimate {
	
	export class Options {
	    infillPercent: number;
	    wallLoops: number;
	    lineWidthMm: number;
	    filamentDiameterMm: number;
	    defaultMaterialId: string;
	    partMaterials: Record<number, string>;
	    currency: string;
	
	    static createFrom(source: any = {}) {
	        return new Options(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.infillPercent = source["infillPercent"];
	        this.wallLoops = source["wallLoops"];
	        this.lineWidthMm = source["lineWidthMm"];
	        this.filamentDiameterMm = source["filamentDiameterMm"];
	        this.defaultMaterialId = source["defaultMaterialId"];
	        this.partMaterials = source["partMaterials"];
	        this.currency = source["currency"];
	    }
	}
	export class PartResult {
	    index: number;
	    materialId: string;
	    volumeMm3: number;
	    effectiveMm3: number;
	    massG: number;
	    lengthM: number;
	    cost: number;
	
	    static createFrom(source: any = {}) {
	        return new PartResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.materialId = source["materialId"];
	        this.volumeMm3 = source["volumeMm3"];
	        this.effectiveMm3 = source["effectiveMm3"];
	        this.massG = source["massG"];
	        this.lengthM = source["lengthM"];
	        this.cost = source["cost"];
	    }
	}
	export class Total {
	    massG: number;
	    lengthM: number;
	    cost: number;
	
	    static createFrom(source: any = {}) {
	        return new Total(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.massG = source["massG"];
	        this.lengthM = source["lengthM"];
	        this.cost = source["cost"];
	    }
	}
	export class Result {
	    parts: PartResult[];
	    total: Total;
	    currency: string;
	    assumptions: string[];
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.parts = this.convertValues(source["parts"], PartResult);
	        this.total = this.convertValues(source["total"], Total);
	        this.currency = source["currency"];
	        this.assumptions = source["assumptions"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace exporter {
	
	export class PartInfo {
	    name: string;
	    color: string;
	    triangles: number;
	    extruder: number;
	    uncolored: boolean;
	
	    static createFrom(source: any = {}) {
	        return new PartInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.color = source["color"];
	        this.triangles = source["triangles"];
	        this.extruder = source["extruder"];
	        this.uncolored = source["uncolored"];
	    }
	}
	export class Preview {
	    parts: PartInfo[];
	    warnings: string[];
	
	    static createFrom(source: any = {}) {
	        return new Preview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.parts = this.convertValues(source["parts"], PartInfo);
	        this.warnings = source["warnings"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Result {
	    path: string;
	    format: string;
	    parts: PartInfo[];
	    warnings: string[];
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.format = source["format"];
	        this.parts = this.convertValues(source["parts"], PartInfo);
	        this.warnings = source["warnings"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace main {
	
	export class AgentChoice {
	    id: string;
	    name: string;
	    found: boolean;
	    path: string;
	    enabled: boolean;
	    canList: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AgentChoice(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.found = source["found"];
	        this.path = source["path"];
	        this.enabled = source["enabled"];
	        this.canList = source["canList"];
	    }
	}
	export class AntigravityMCP {
	    state: string;
	    agyPath: string;
	    command: string;
	    current: string;
	    detail: string;
	
	    static createFrom(source: any = {}) {
	        return new AntigravityMCP(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.state = source["state"];
	        this.agyPath = source["agyPath"];
	        this.command = source["command"];
	        this.current = source["current"];
	        this.detail = source["detail"];
	    }
	}
	export class Bootstrap {
	    version: string;
	    platform: string;
	    dataDir: string;
	    schemaVersion: number;
	    settings: store.Settings;
	    initError: string;
	
	    static createFrom(source: any = {}) {
	        return new Bootstrap(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.platform = source["platform"];
	        this.dataDir = source["dataDir"];
	        this.schemaVersion = source["schemaVersion"];
	        this.settings = this.convertValues(source["settings"], store.Settings);
	        this.initError = source["initError"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ChatView {
	    chat: store.Chat;
	    messages: store.ChatMessage[];
	    running: boolean;
	    partial: string;
	    partialSeq: number;
	
	    static createFrom(source: any = {}) {
	        return new ChatView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.chat = this.convertValues(source["chat"], store.Chat);
	        this.messages = this.convertValues(source["messages"], store.ChatMessage);
	        this.running = source["running"];
	        this.partial = source["partial"];
	        this.partialSeq = source["partialSeq"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CheckpointPair {
	    file: string;
	    parent: string;
	    parentSource: string;
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new CheckpointPair(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.file = source["file"];
	        this.parent = source["parent"];
	        this.parentSource = source["parentSource"];
	        this.source = source["source"];
	    }
	}
	export class CheckpointResume {
	    kind: string;
	    terminalId: string;
	    agentId: string;
	    chatId: number;
	
	    static createFrom(source: any = {}) {
	        return new CheckpointResume(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.terminalId = source["terminalId"];
	        this.agentId = source["agentId"];
	        this.chatId = source["chatId"];
	    }
	}
	export class MCPStatus {
	    enabled: boolean;
	    binary: string;
	    binaryError: string;
	    workspaceDir: string;
	    command: string;
	    files: mcpconf.FileReport[];
	    codexFlags: string[];
	    openscad: boolean;
	    openscadPath: string;
	
	    static createFrom(source: any = {}) {
	        return new MCPStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.binary = source["binary"];
	        this.binaryError = source["binaryError"];
	        this.workspaceDir = source["workspaceDir"];
	        this.command = source["command"];
	        this.files = this.convertValues(source["files"], mcpconf.FileReport);
	        this.codexFlags = source["codexFlags"];
	        this.openscad = source["openscad"];
	        this.openscadPath = source["openscadPath"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class OpenProjectEntry {
	    workspace: store.Workspace;
	    missing: boolean;
	
	    static createFrom(source: any = {}) {
	        return new OpenProjectEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.workspace = this.convertValues(source["workspace"], store.Workspace);
	        this.missing = source["missing"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class OpenProjectsView {
	    projects: OpenProjectEntry[];
	    activeId: number;
	
	    static createFrom(source: any = {}) {
	        return new OpenProjectsView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projects = this.convertValues(source["projects"], OpenProjectEntry);
	        this.activeId = source["activeId"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ProjectActivityEntry {
	    id: number;
	    rendering: boolean;
	    latestFile: string;
	    lastRenderStatus: string;
	    renderFailed: boolean;
	    agentsAlive: number;
	
	    static createFrom(source: any = {}) {
	        return new ProjectActivityEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.rendering = source["rendering"];
	        this.latestFile = source["latestFile"];
	        this.lastRenderStatus = source["lastRenderStatus"];
	        this.renderFailed = source["renderFailed"];
	        this.agentsAlive = source["agentsAlive"];
	    }
	}
	export class SkillView {
	    name: string;
	    description: string;
	    source: string;
	    dir: string;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SkillView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.source = source["source"];
	        this.dir = source["dir"];
	        this.enabled = source["enabled"];
	    }
	}
	export class Slicer {
	    id: string;
	    name: string;
	    path: string;
	    found: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Slicer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.found = source["found"];
	    }
	}
	export class TerminalInfo {
	    id: string;
	    kind: string;
	    agentId: string;
	    workspaceId: number;
	    cols: number;
	    rows: number;
	    startedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new TerminalInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.agentId = source["agentId"];
	        this.workspaceId = source["workspaceId"];
	        this.cols = source["cols"];
	        this.rows = source["rows"];
	        this.startedAt = source["startedAt"];
	    }
	}
	export class TerminalSnapshot {
	    data: string;
	    seq: number;
	
	    static createFrom(source: any = {}) {
	        return new TerminalSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.data = source["data"];
	        this.seq = source["seq"];
	    }
	}
	export class WorkspaceView {
	    workspace: store.Workspace;
	    checkpoints: store.Checkpoint[];
	    missing: boolean;
	    activeParent: string;
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.workspace = this.convertValues(source["workspace"], store.Workspace);
	        this.checkpoints = this.convertValues(source["checkpoints"], store.Checkpoint);
	        this.missing = source["missing"];
	        this.activeParent = source["activeParent"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace mcpconf {
	
	export class FileReport {
	    agent: string;
	    name: string;
	    path: string;
	    status: string;
	    detail?: string;
	
	    static createFrom(source: any = {}) {
	        return new FileReport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.agent = source["agent"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.status = source["status"];
	        this.detail = source["detail"];
	    }
	}
	export class Report {
	    files: FileReport[];
	
	    static createFrom(source: any = {}) {
	        return new Report(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.files = this.convertValues(source["files"], FileReport);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace mesh {
	
	export class Info {
	    triangles: number;
	    min: number[];
	    max: number[];
	    size: number[];
	    volumeMm3: number;
	    areaMm2: number;
	    watertight: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Info(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.triangles = source["triangles"];
	        this.min = source["min"];
	        this.max = source["max"];
	        this.size = source["size"];
	        this.volumeMm3 = source["volumeMm3"];
	        this.areaMm2 = source["areaMm2"];
	        this.watertight = source["watertight"];
	    }
	}

}

export namespace parts {
	
	export class Part {
	    index: number;
	    name: string;
	    color: string;
	    uncolored: boolean;
	    url: string;
	    info: mesh.Info;
	
	    static createFrom(source: any = {}) {
	        return new Part(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.index = source["index"];
	        this.name = source["name"];
	        this.color = source["color"];
	        this.uncolored = source["uncolored"];
	        this.url = source["url"];
	        this.info = this.convertValues(source["info"], mesh.Info);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Model {
	    key: string;
	    parts: Part[];
	    total: mesh.Info;
	    plate: printers.Volume;
	    fits: boolean;
	    overflow: printers.Volume;
	
	    static createFrom(source: any = {}) {
	        return new Model(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.key = source["key"];
	        this.parts = this.convertValues(source["parts"], Part);
	        this.total = this.convertValues(source["total"], mesh.Info);
	        this.plate = this.convertValues(source["plate"], printers.Volume);
	        this.fits = source["fits"];
	        this.overflow = this.convertValues(source["overflow"], printers.Volume);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace printers {
	
	export class Volume {
	    x: number;
	    y: number;
	    z: number;
	
	    static createFrom(source: any = {}) {
	        return new Volume(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.x = source["x"];
	        this.y = source["y"];
	        this.z = source["z"];
	    }
	}
	export class Printer {
	    id: string;
	    name: string;
	    build_volume: Volume;
	    nozzle_mm: number;
	    multicolor_slots: number;
	    multicolor_system: string;
	    enclosed: boolean;
	    heated_chamber: boolean;
	    notes: string;
	    verify: string[];
	
	    static createFrom(source: any = {}) {
	        return new Printer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.build_volume = this.convertValues(source["build_volume"], Volume);
	        this.nozzle_mm = source["nozzle_mm"];
	        this.multicolor_slots = source["multicolor_slots"];
	        this.multicolor_system = source["multicolor_system"];
	        this.enclosed = source["enclosed"];
	        this.heated_chamber = source["heated_chamber"];
	        this.notes = source["notes"];
	        this.verify = source["verify"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace render {
	
	export class Diagnostic {
	    severity: string;
	    line: number;
	    col: number;
	    file: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new Diagnostic(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.severity = source["severity"];
	        this.line = source["line"];
	        this.col = source["col"];
	        this.file = source["file"];
	        this.message = source["message"];
	    }
	}
	export class Result {
	    fileName: string;
	    status: string;
	    meshPath: string;
	    cacheHit: boolean;
	    durationMs: number;
	    diagnostics: Diagnostic[];
	    log: string;
	    backend: string;
	    version: string;
	
	    static createFrom(source: any = {}) {
	        return new Result(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fileName = source["fileName"];
	        this.status = source["status"];
	        this.meshPath = source["meshPath"];
	        this.cacheHit = source["cacheHit"];
	        this.durationMs = source["durationMs"];
	        this.diagnostics = this.convertValues(source["diagnostics"], Diagnostic);
	        this.log = source["log"];
	        this.backend = source["backend"];
	        this.version = source["version"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace scad {
	
	export class LocalizedText {
	    es: string;
	    en: string;
	
	    static createFrom(source: any = {}) {
	        return new LocalizedText(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.es = source["es"];
	        this.en = source["en"];
	    }
	}
	export class CatalogParam {
	    name: string;
	    doc: LocalizedText;
	
	    static createFrom(source: any = {}) {
	        return new CatalogParam(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.doc = this.convertValues(source["doc"], LocalizedText);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class CatalogEntry {
	    name: string;
	    kind: string;
	    signature: string;
	    params: CatalogParam[];
	    doc: LocalizedText;
	    since?: string;
	
	    static createFrom(source: any = {}) {
	        return new CatalogEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.signature = source["signature"];
	        this.params = this.convertValues(source["params"], CatalogParam);
	        this.doc = this.convertValues(source["doc"], LocalizedText);
	        this.since = source["since"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Catalog {
	    version: string;
	    entries: CatalogEntry[];
	
	    static createFrom(source: any = {}) {
	        return new Catalog(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.entries = this.convertValues(source["entries"], CatalogEntry);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	
	export class Diagnostic {
	    severity: string;
	    line: number;
	    col: number;
	    endLine: number;
	    endCol: number;
	    code: string;
	    source: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new Diagnostic(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.severity = source["severity"];
	        this.line = source["line"];
	        this.col = source["col"];
	        this.endLine = source["endLine"];
	        this.endCol = source["endCol"];
	        this.code = source["code"];
	        this.source = source["source"];
	        this.message = source["message"];
	    }
	}

}

export namespace store {
	
	export class AIProvider {
	    id: string;
	    kind: string;
	    name: string;
	    command: string;
	    config: Record<string, any>;
	    enabled: boolean;
	    sortOrder: number;
	
	    static createFrom(source: any = {}) {
	        return new AIProvider(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.name = source["name"];
	        this.command = source["command"];
	        this.config = source["config"];
	        this.enabled = source["enabled"];
	        this.sortOrder = source["sortOrder"];
	    }
	}
	export class Appearance {
	    uiScale: number;
	    editorFontSize: number;
	    terminalFontSize: number;
	    chatFontSize: number;
	
	    static createFrom(source: any = {}) {
	        return new Appearance(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.uiScale = source["uiScale"];
	        this.editorFontSize = source["editorFontSize"];
	        this.terminalFontSize = source["terminalFontSize"];
	        this.chatFontSize = source["chatFontSize"];
	    }
	}
	export class Chat {
	    id: number;
	    workspaceId: number;
	    providerId: string;
	    model: string;
	    title: string;
	    createdAt: number;
	    updatedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new Chat(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.workspaceId = source["workspaceId"];
	        this.providerId = source["providerId"];
	        this.model = source["model"];
	        this.title = source["title"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class ChatMessage {
	    id: number;
	    chatId: number;
	    role: string;
	    content: string;
	    createdAt: number;
	
	    static createFrom(source: any = {}) {
	        return new ChatMessage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.chatId = source["chatId"];
	        this.role = source["role"];
	        this.content = source["content"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class Checkpoint {
	    id: number;
	    workspaceId: number;
	    fileName: string;
	    parentFile: string;
	    label: string;
	    description: string;
	    sessionId: string;
	    agentId: string;
	    chatId: number;
	    collapsed: boolean;
	    fileMtime: number;
	    createdAt: number;
	
	    static createFrom(source: any = {}) {
	        return new Checkpoint(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.workspaceId = source["workspaceId"];
	        this.fileName = source["fileName"];
	        this.parentFile = source["parentFile"];
	        this.label = source["label"];
	        this.description = source["description"];
	        this.sessionId = source["sessionId"];
	        this.agentId = source["agentId"];
	        this.chatId = source["chatId"];
	        this.collapsed = source["collapsed"];
	        this.fileMtime = source["fileMtime"];
	        this.createdAt = source["createdAt"];
	    }
	}
	export class DockLayoutEntry {
	    name: string;
	    json: string;
	
	    static createFrom(source: any = {}) {
	        return new DockLayoutEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.json = source["json"];
	    }
	}
	export class Layout {
	    sidebarWidth: number;
	    sidebarCollapsed: boolean;
	    codeWidth: number;
	    bottomHeight: number;
	    dimsCollapsed: boolean;
	    statusBarHidden: boolean;
	    workspacesHeight: number;
	
	    static createFrom(source: any = {}) {
	        return new Layout(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sidebarWidth = source["sidebarWidth"];
	        this.sidebarCollapsed = source["sidebarCollapsed"];
	        this.codeWidth = source["codeWidth"];
	        this.bottomHeight = source["bottomHeight"];
	        this.dimsCollapsed = source["dimsCollapsed"];
	        this.statusBarHidden = source["statusBarHidden"];
	        this.workspacesHeight = source["workspacesHeight"];
	    }
	}
	export class Material {
	    id: string;
	    name: string;
	    densityGCm3: number;
	    costPerKg: number;
	
	    static createFrom(source: any = {}) {
	        return new Material(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.densityGCm3 = source["densityGCm3"];
	        this.costPerKg = source["costPerKg"];
	    }
	}
	export class ViewerState {
	    wireframe: boolean;
	    edges: boolean;
	    ortho: boolean[];
	    split: boolean;
	    colorIndex: number;
	    modelColors: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ViewerState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.wireframe = source["wireframe"];
	        this.edges = source["edges"];
	        this.ortho = source["ortho"];
	        this.split = source["split"];
	        this.colorIndex = source["colorIndex"];
	        this.modelColors = source["modelColors"];
	    }
	}
	export class Settings {
	    language: string;
	    theme: string;
	    openscadPath: string;
	    renderBackend: string;
	    workspacesRoot: string;
	    defaultPrinter: string;
	    defaultMaterial: string;
	    infillPercent: number;
	    defaultAgent: string;
	    wallLoops: number;
	    lineWidthMm: number;
	    filamentDiameterMm: number;
	    currency: string;
	    bambuStudioPath: string;
	    autoRender: boolean;
	    renderTimeoutSec: number;
	    autoIterate: boolean;
	    mcpEnabled: boolean;
	    swatches: string[];
	    editorIndent: number;
	    editorFontSize: number;
	    editorWordWrap: boolean;
	    editorMinimap: boolean;
	    uiScale: number;
	    terminalFontSize: number;
	    chatFontSize: number;
	    layout: Layout;
	    dockLayout: string;
	    dockLayouts: DockLayoutEntry[];
	    openProjects: number[];
	    activeProject: number;
	    viewer: ViewerState;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.language = source["language"];
	        this.theme = source["theme"];
	        this.openscadPath = source["openscadPath"];
	        this.renderBackend = source["renderBackend"];
	        this.workspacesRoot = source["workspacesRoot"];
	        this.defaultPrinter = source["defaultPrinter"];
	        this.defaultMaterial = source["defaultMaterial"];
	        this.infillPercent = source["infillPercent"];
	        this.defaultAgent = source["defaultAgent"];
	        this.wallLoops = source["wallLoops"];
	        this.lineWidthMm = source["lineWidthMm"];
	        this.filamentDiameterMm = source["filamentDiameterMm"];
	        this.currency = source["currency"];
	        this.bambuStudioPath = source["bambuStudioPath"];
	        this.autoRender = source["autoRender"];
	        this.renderTimeoutSec = source["renderTimeoutSec"];
	        this.autoIterate = source["autoIterate"];
	        this.mcpEnabled = source["mcpEnabled"];
	        this.swatches = source["swatches"];
	        this.editorIndent = source["editorIndent"];
	        this.editorFontSize = source["editorFontSize"];
	        this.editorWordWrap = source["editorWordWrap"];
	        this.editorMinimap = source["editorMinimap"];
	        this.uiScale = source["uiScale"];
	        this.terminalFontSize = source["terminalFontSize"];
	        this.chatFontSize = source["chatFontSize"];
	        this.layout = this.convertValues(source["layout"], Layout);
	        this.dockLayout = source["dockLayout"];
	        this.dockLayouts = this.convertValues(source["dockLayouts"], DockLayoutEntry);
	        this.openProjects = source["openProjects"];
	        this.activeProject = source["activeProject"];
	        this.viewer = this.convertValues(source["viewer"], ViewerState);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class Workspace {
	    id: number;
	    name: string;
	    path: string;
	    printerId: string;
	    description: string;
	    lastOpened: number;
	    createdAt: number;
	    lastCheckpoint: string;
	
	    static createFrom(source: any = {}) {
	        return new Workspace(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.path = source["path"];
	        this.printerId = source["printerId"];
	        this.description = source["description"];
	        this.lastOpened = source["lastOpened"];
	        this.createdAt = source["createdAt"];
	        this.lastCheckpoint = source["lastCheckpoint"];
	    }
	}

}


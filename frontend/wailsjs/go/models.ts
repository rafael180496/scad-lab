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
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new Tool(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.found = source["found"];
	        this.path = source["path"];
	        this.version = source["version"];
	        this.source = source["source"];
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

export namespace hermes {
	
	export class Attachment {
	    id: string;
	    kind: string;
	    name: string;
	    mime: string;
	    size: number;
	    thumbDataUrl: string;
	    refPath: string;
	    summary: string;
	
	    static createFrom(source: any = {}) {
	        return new Attachment(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.name = source["name"];
	        this.mime = source["mime"];
	        this.size = source["size"];
	        this.thumbDataUrl = source["thumbDataUrl"];
	        this.refPath = source["refPath"];
	        this.summary = source["summary"];
	    }
	}
	export class Step {
	    id: string;
	    index: number;
	    tool: string;
	    args: Record<string, any>;
	    status: string;
	    text: string;
	    images: string[];
	    durationMs: number;
	    textAt: number;
	
	    static createFrom(source: any = {}) {
	        return new Step(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.index = source["index"];
	        this.tool = source["tool"];
	        this.args = source["args"];
	        this.status = source["status"];
	        this.text = source["text"];
	        this.images = source["images"];
	        this.durationMs = source["durationMs"];
	        this.textAt = source["textAt"];
	    }
	}
	export class Usage {
	    inputTokens: number;
	    outputTokens: number;
	    cacheRead: number;
	    cacheWrite: number;
	    thinking: number;
	    costUsd: number;
	
	    static createFrom(source: any = {}) {
	        return new Usage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.inputTokens = source["inputTokens"];
	        this.outputTokens = source["outputTokens"];
	        this.cacheRead = source["cacheRead"];
	        this.cacheWrite = source["cacheWrite"];
	        this.thinking = source["thinking"];
	        this.costUsd = source["costUsd"];
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
	export class AntigravityEnsure {
	    added: boolean;
	    state: string;
	
	    static createFrom(source: any = {}) {
	        return new AntigravityEnsure(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.added = source["added"];
	        this.state = source["state"];
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
	export class ChatProvider {
	    id: string;
	    kind: string;
	    name: string;
	    vendor: string;
	    installed: boolean;
	    path: string;
	    remote: boolean;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ChatProvider(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.name = source["name"];
	        this.vendor = source["vendor"];
	        this.installed = source["installed"];
	        this.path = source["path"];
	        this.remote = source["remote"];
	        this.enabled = source["enabled"];
	    }
	}
	export class ChatView {
	    chat: store.Chat;
	    messages: store.ChatMessage[];
	    running: boolean;
	    partial: string;
	    partialSeq: number;
	    steps: hermes.Step[];
	    usage: hermes.Usage;
	
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
	        this.steps = this.convertValues(source["steps"], hermes.Step);
	        this.usage = this.convertValues(source["usage"], hermes.Usage);
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
	export class DeleteSkillResult {
	    trash: string;
	    removed: number;
	    kept: string[];
	    syncError: string;
	
	    static createFrom(source: any = {}) {
	        return new DeleteSkillResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.trash = source["trash"];
	        this.removed = source["removed"];
	        this.kept = source["kept"];
	        this.syncError = source["syncError"];
	    }
	}
	export class DetectedCLI {
	    id: string;
	    name: string;
	    vendor: string;
	    installed: boolean;
	    path: string;
	    version: string;
	
	    static createFrom(source: any = {}) {
	        return new DetectedCLI(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.vendor = source["vendor"];
	        this.installed = source["installed"];
	        this.path = source["path"];
	        this.version = source["version"];
	    }
	}
	export class EditBase {
	    ref: string;
	    parent: string;
	    meshPath: string;
	    report: refmodel.Report;
	    fromImport: boolean;
	    model: meshedit.Transform;
	    color: string;
	
	    static createFrom(source: any = {}) {
	        return new EditBase(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ref = source["ref"];
	        this.parent = source["parent"];
	        this.meshPath = source["meshPath"];
	        this.report = this.convertValues(source["report"], refmodel.Report);
	        this.fromImport = source["fromImport"];
	        this.model = this.convertValues(source["model"], meshedit.Transform);
	        this.color = source["color"];
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
	export class ImportOptions {
	    centerOnBed: boolean;
	    label: string;
	
	    static createFrom(source: any = {}) {
	        return new ImportOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.centerOnBed = source["centerOnBed"];
	        this.label = source["label"];
	    }
	}
	export class ImportResult {
	    file: string;
	    ref: string;
	    report: refmodel.Report;
	    warnings: string[];
	
	    static createFrom(source: any = {}) {
	        return new ImportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.file = source["file"];
	        this.ref = source["ref"];
	        this.report = this.convertValues(source["report"], refmodel.Report);
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
	export class LastModel {
	    serverId: string;
	    model: string;
	
	    static createFrom(source: any = {}) {
	        return new LastModel(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serverId = source["serverId"];
	        this.model = source["model"];
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
	export class MiniToolsServer {
	    name: string;
	    kind: string;
	    baseUrl: string;
	    defaultModel: string;
	    contextTokens?: number;
	    exists: boolean;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new MiniToolsServer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.kind = source["kind"];
	        this.baseUrl = source["baseUrl"];
	        this.defaultModel = source["defaultModel"];
	        this.contextTokens = source["contextTokens"];
	        this.exists = source["exists"];
	        this.error = source["error"];
	    }
	}
	export class MiniToolsImport {
	    found: boolean;
	    path: string;
	    servers: MiniToolsServer[];
	    imported: number;
	
	    static createFrom(source: any = {}) {
	        return new MiniToolsImport(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.found = source["found"];
	        this.path = source["path"];
	        this.servers = this.convertValues(source["servers"], MiniToolsServer);
	        this.imported = source["imported"];
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
	
	export class ModelInfo {
	    id: string;
	    label: string;
	    description: string;
	    vision: boolean;
	    visionKnown: boolean;
	    tools: boolean;
	    toolsKnown: boolean;
	    contextTokens: number;
	    promptPricePerM: number;
	    completionPricePerM: number;
	
	    static createFrom(source: any = {}) {
	        return new ModelInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.label = source["label"];
	        this.description = source["description"];
	        this.vision = source["vision"];
	        this.visionKnown = source["visionKnown"];
	        this.tools = source["tools"];
	        this.toolsKnown = source["toolsKnown"];
	        this.contextTokens = source["contextTokens"];
	        this.promptPricePerM = source["promptPricePerM"];
	        this.completionPricePerM = source["completionPricePerM"];
	    }
	}
	export class ModelCatalog {
	    defaultHint: string;
	    models: ModelInfo[];
	
	    static createFrom(source: any = {}) {
	        return new ModelCatalog(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.defaultHint = source["defaultHint"];
	        this.models = this.convertValues(source["models"], ModelInfo);
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
	export class OpenSCADDownload {
	    version: string;
	    sizeBytes: number;
	    supported: boolean;
	    installed: boolean;
	    installedPath: string;
	    running: boolean;
	    wasmVersion: string;
	
	    static createFrom(source: any = {}) {
	        return new OpenSCADDownload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.sizeBytes = source["sizeBytes"];
	        this.supported = source["supported"];
	        this.installed = source["installed"];
	        this.installedPath = source["installedPath"];
	        this.running = source["running"];
	        this.wasmVersion = source["wasmVersion"];
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
	export class ProviderHealthInfo {
	    ok: boolean;
	    latencyMs: number;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new ProviderHealthInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.latencyMs = source["latencyMs"];
	        this.error = source["error"];
	    }
	}
	export class RenameSkillResult {
	    name: string;
	    removed: number;
	    kept: string[];
	    syncError: string;
	
	    static createFrom(source: any = {}) {
	        return new RenameSkillResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.removed = source["removed"];
	        this.kept = source["kept"];
	        this.syncError = source["syncError"];
	    }
	}
	export class SaveSkillFileResult {
	    saved: boolean;
	    diagnostics: skills.Diagnostic[];
	    syncError: string;
	
	    static createFrom(source: any = {}) {
	        return new SaveSkillFileResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.saved = source["saved"];
	        this.diagnostics = this.convertValues(source["diagnostics"], skills.Diagnostic);
	        this.syncError = source["syncError"];
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
	export class SendOptions {
	    agent?: boolean;
	    maxSteps: number;
	
	    static createFrom(source: any = {}) {
	        return new SendOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.agent = source["agent"];
	        this.maxSteps = source["maxSteps"];
	    }
	}
	export class ServerProbe {
	    ok: boolean;
	    status: number;
	    message: string;
	    models: number;
	    modelIds: string[];
	    latencyMs: number;
	
	    static createFrom(source: any = {}) {
	        return new ServerProbe(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ok = source["ok"];
	        this.status = source["status"];
	        this.message = source["message"];
	        this.models = source["models"];
	        this.modelIds = source["modelIds"];
	        this.latencyMs = source["latencyMs"];
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
	export class SkillsFolder {
	    path: string;
	    default: string;
	    custom: boolean;
	    exists: boolean;
	    count: number;
	
	    static createFrom(source: any = {}) {
	        return new SkillsFolder(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.default = source["default"];
	        this.custom = source["custom"];
	        this.exists = source["exists"];
	        this.count = source["count"];
	    }
	}
	export class SkillsFolderChange {
	    path: string;
	    moved: string[];
	    skipped: string[];
	    syncError: string;
	
	    static createFrom(source: any = {}) {
	        return new SkillsFolderChange(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.moved = source["moved"];
	        this.skipped = source["skipped"];
	        this.syncError = source["syncError"];
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
	export class VaultResult {
	    rememberFailed: boolean;
	    detail: string;
	    lostKeys: number;
	
	    static createFrom(source: any = {}) {
	        return new VaultResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.rememberFailed = source["rememberFailed"];
	        this.detail = source["detail"];
	        this.lostKeys = source["lostKeys"];
	    }
	}
	export class VaultStatus {
	    initialized: boolean;
	    unlocked: boolean;
	    remembered: boolean;
	    keyringAvailable: boolean;
	    keyringDetail: string;
	
	    static createFrom(source: any = {}) {
	        return new VaultStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.initialized = source["initialized"];
	        this.unlocked = source["unlocked"];
	        this.remembered = source["remembered"];
	        this.keyringAvailable = source["keyringAvailable"];
	        this.keyringDetail = source["keyringDetail"];
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

export namespace meshedit {
	
	export class Bounds {
	    min: number[];
	    max: number[];
	
	    static createFrom(source: any = {}) {
	        return new Bounds(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.min = source["min"];
	        this.max = source["max"];
	    }
	}
	export class Cut {
	    point: number[];
	    normal: number[];
	    keep: string;
	    split: string;
	    colors: string[];
	
	    static createFrom(source: any = {}) {
	        return new Cut(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.point = source["point"];
	        this.normal = source["normal"];
	        this.keep = source["keep"];
	        this.split = source["split"];
	        this.colors = source["colors"];
	    }
	}
	export class Transform {
	    translate: number[];
	    rotate: number[];
	    scale: number[];
	    mirror: boolean[];
	
	    static createFrom(source: any = {}) {
	        return new Transform(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.translate = source["translate"];
	        this.rotate = source["rotate"];
	        this.scale = source["scale"];
	        this.mirror = source["mirror"];
	    }
	}
	export class Primitive {
	    id: string;
	    shape: string;
	    mode: string;
	    size: number[];
	    text: string;
	    center: boolean;
	    color: string;
	    transform: Transform;
	
	    static createFrom(source: any = {}) {
	        return new Primitive(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.shape = source["shape"];
	        this.mode = source["mode"];
	        this.size = source["size"];
	        this.text = source["text"];
	        this.center = source["center"];
	        this.color = source["color"];
	        this.transform = this.convertValues(source["transform"], Transform);
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
	export class Scene {
	    ref: string;
	    model: Transform;
	    color: string;
	    parts: Primitive[];
	    cut?: Cut;
	    ops: string[];
	
	    static createFrom(source: any = {}) {
	        return new Scene(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ref = source["ref"];
	        this.model = this.convertValues(source["model"], Transform);
	        this.color = source["color"];
	        this.parts = this.convertValues(source["parts"], Primitive);
	        this.cut = this.convertValues(source["cut"], Cut);
	        this.ops = source["ops"];
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

export namespace refmodel {
	
	export class Part {
	    name: string;
	    color: string;
	    triangles: number;
	    size: number[];
	    volumeMm3: number;
	
	    static createFrom(source: any = {}) {
	        return new Part(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.color = source["color"];
	        this.triangles = source["triangles"];
	        this.size = source["size"];
	        this.volumeMm3 = source["volumeMm3"];
	    }
	}
	export class Report {
	    format: string;
	    triangles: number;
	    min: number[];
	    max: number[];
	    size: number[];
	    volumeMm3: number;
	    areaMm2: number;
	    watertight: boolean;
	    parts: Part[];
	
	    static createFrom(source: any = {}) {
	        return new Report(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.format = source["format"];
	        this.triangles = source["triangles"];
	        this.min = source["min"];
	        this.max = source["max"];
	        this.size = source["size"];
	        this.volumeMm3 = source["volumeMm3"];
	        this.areaMm2 = source["areaMm2"];
	        this.watertight = source["watertight"];
	        this.parts = this.convertValues(source["parts"], Part);
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
	    engine: string;
	
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
	        this.engine = source["engine"];
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

export namespace skills {
	
	export class Diagnostic {
	    line: number;
	    severity: string;
	    code: string;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new Diagnostic(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.line = source["line"];
	        this.severity = source["severity"];
	        this.code = source["code"];
	        this.message = source["message"];
	    }
	}
	export class File {
	    path: string;
	    size: number;
	    text: boolean;
	
	    static createFrom(source: any = {}) {
	        return new File(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.size = source["size"];
	        this.text = source["text"];
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
	    cliSession: string;
	    mode: string;
	
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
	        this.cliSession = source["cliSession"];
	        this.mode = source["mode"];
	    }
	}
	export class ChatMessage {
	    id: number;
	    chatId: number;
	    role: string;
	    content: string;
	    createdAt: number;
	    parts: string;
	    inputTokens: number;
	    outputTokens: number;
	    costUsd: number;
	
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
	        this.parts = source["parts"];
	        this.inputTokens = source["inputTokens"];
	        this.outputTokens = source["outputTokens"];
	        this.costUsd = source["costUsd"];
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
	export class ModelServer {
	    id: string;
	    kind: string;
	    preset: string;
	    name: string;
	    baseUrl: string;
	    apiKeyEnv: string;
	    hasToken: boolean;
	    defaultModel: string;
	    contextTokens: number;
	    enabled: boolean;
	    sortOrder: number;
	    updatedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new ModelServer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.preset = source["preset"];
	        this.name = source["name"];
	        this.baseUrl = source["baseUrl"];
	        this.apiKeyEnv = source["apiKeyEnv"];
	        this.hasToken = source["hasToken"];
	        this.defaultModel = source["defaultModel"];
	        this.contextTokens = source["contextTokens"];
	        this.enabled = source["enabled"];
	        this.sortOrder = source["sortOrder"];
	        this.updatedAt = source["updatedAt"];
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
	    skillsDir: string;
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
	        this.skillsDir = source["skillsDir"];
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


export namespace models {
	
	export class AppSettings {
	    autocompleteEnabled: boolean;
	    autocompleteFromHistory: boolean;
	    autocompleteFromShortcut: boolean;
	    autoOpenSftp: boolean;
	    sftpPanelPosition: string;
	    terminalFontFamily: string;
	    terminalFontSize: number;
	    defaultKeepAliveSeconds: number;
	    maxHistoryEntries: number;
	    recordCommands: boolean;
	    language: string;
	    monitorBarEnabled: boolean;
	    sftpFollowTerminal: boolean;
	    pasteGuardEnabled: boolean;
	    keywordHighlightEnabled: boolean;
	    autoSwitchEnglishIme: boolean;
	    checkUpdatesOnStartup: boolean;
	    syncRelayUrl?: string;
	
	    static createFrom(source: any = {}) {
	        return new AppSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.autocompleteEnabled = source["autocompleteEnabled"];
	        this.autocompleteFromHistory = source["autocompleteFromHistory"];
	        this.autocompleteFromShortcut = source["autocompleteFromShortcut"];
	        this.autoOpenSftp = source["autoOpenSftp"];
	        this.sftpPanelPosition = source["sftpPanelPosition"];
	        this.terminalFontFamily = source["terminalFontFamily"];
	        this.terminalFontSize = source["terminalFontSize"];
	        this.defaultKeepAliveSeconds = source["defaultKeepAliveSeconds"];
	        this.maxHistoryEntries = source["maxHistoryEntries"];
	        this.recordCommands = source["recordCommands"];
	        this.language = source["language"];
	        this.monitorBarEnabled = source["monitorBarEnabled"];
	        this.sftpFollowTerminal = source["sftpFollowTerminal"];
	        this.pasteGuardEnabled = source["pasteGuardEnabled"];
	        this.keywordHighlightEnabled = source["keywordHighlightEnabled"];
	        this.autoSwitchEnglishIme = source["autoSwitchEnglishIme"];
	        this.checkUpdatesOnStartup = source["checkUpdatesOnStartup"];
	        this.syncRelayUrl = source["syncRelayUrl"];
	    }
	}
	export class BuiltinTemplate {
	    id: string;
	    presetId?: string;
	    name: string;
	    category: string;
	    description: string;
	    safetyNote: string;
	    command: string;
	
	    static createFrom(source: any = {}) {
	        return new BuiltinTemplate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.presetId = source["presetId"];
	        this.name = source["name"];
	        this.category = source["category"];
	        this.description = source["description"];
	        this.safetyNote = source["safetyNote"];
	        this.command = source["command"];
	    }
	}
	export class CommandRecord {
	    id: string;
	    command: string;
	    serverId: string;
	    serverName: string;
	    sessionId: string;
	    // Go type: time
	    executedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new CommandRecord(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.command = source["command"];
	        this.serverId = source["serverId"];
	        this.serverName = source["serverName"];
	        this.sessionId = source["sessionId"];
	        this.executedAt = this.convertValues(source["executedAt"], null);
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
	export class CommandShortcut {
	    id: string;
	    name: string;
	    command: string;
	    description: string;
	    category: string;
	    autoExecute: boolean;
	    pinned: boolean;
	    useCount: number;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    updatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new CommandShortcut(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.command = source["command"];
	        this.description = source["description"];
	        this.category = source["category"];
	        this.autoExecute = source["autoExecute"];
	        this.pinned = source["pinned"];
	        this.useCount = source["useCount"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
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
	export class CompletionItem {
	    command: string;
	    description: string;
	    category: string;
	    source: string;
	    insertText: string;
	    score: number;
	
	    static createFrom(source: any = {}) {
	        return new CompletionItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.command = source["command"];
	        this.description = source["description"];
	        this.category = source["category"];
	        this.source = source["source"];
	        this.insertText = source["insertText"];
	        this.score = source["score"];
	    }
	}
	export class HostMetrics {
	    sessionId: string;
	    supported: boolean;
	    latencyMs: number;
	    cpuPercent: number;
	    memUsedMb: number;
	    memTotalMb: number;
	    memPercent: number;
	    diskMount: string;
	    diskPercent: number;
	    netRxKbps: number;
	    netTxKbps: number;
	    load1: number;
	    cores: number;
	    hasGpu: boolean;
	    gpuUtil: number;
	    gpuName?: string;
	    // Go type: time
	    updatedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new HostMetrics(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionId = source["sessionId"];
	        this.supported = source["supported"];
	        this.latencyMs = source["latencyMs"];
	        this.cpuPercent = source["cpuPercent"];
	        this.memUsedMb = source["memUsedMb"];
	        this.memTotalMb = source["memTotalMb"];
	        this.memPercent = source["memPercent"];
	        this.diskMount = source["diskMount"];
	        this.diskPercent = source["diskPercent"];
	        this.netRxKbps = source["netRxKbps"];
	        this.netTxKbps = source["netTxKbps"];
	        this.load1 = source["load1"];
	        this.cores = source["cores"];
	        this.hasGpu = source["hasGpu"];
	        this.gpuUtil = source["gpuUtil"];
	        this.gpuName = source["gpuName"];
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
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
	export class JumpHostView {
	    mode: string;
	    serverId?: string;
	    host?: string;
	    port?: number;
	    username?: string;
	    authType?: string;
	    hasPassword: boolean;
	    sshKeyId?: string;
	    useMainProxy?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new JumpHostView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.serverId = source["serverId"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.username = source["username"];
	        this.authType = source["authType"];
	        this.hasPassword = source["hasPassword"];
	        this.sshKeyId = source["sshKeyId"];
	        this.useMainProxy = source["useMainProxy"];
	    }
	}
	export class JumpSaveReq {
	    mode: string;
	    serverId: string;
	    host: string;
	    port: number;
	    username: string;
	    authType: string;
	    password: string;
	    keepExistingPassword: boolean;
	    sshKeyId: string;
	    useMainProxy: boolean;
	
	    static createFrom(source: any = {}) {
	        return new JumpSaveReq(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.serverId = source["serverId"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.username = source["username"];
	        this.authType = source["authType"];
	        this.password = source["password"];
	        this.keepExistingPassword = source["keepExistingPassword"];
	        this.sshKeyId = source["sshKeyId"];
	        this.useMainProxy = source["useMainProxy"];
	    }
	}
	export class KeyDeployResult {
	    serverId: string;
	    serverName: string;
	    ok: boolean;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new KeyDeployResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.serverId = source["serverId"];
	        this.serverName = source["serverName"];
	        this.ok = source["ok"];
	        this.message = source["message"];
	    }
	}
	export class PresetLoadResult {
	    added: number;
	    skipped: number;
	    templates: BuiltinTemplate[];
	
	    static createFrom(source: any = {}) {
	        return new PresetLoadResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.added = source["added"];
	        this.skipped = source["skipped"];
	        this.templates = this.convertValues(source["templates"], BuiltinTemplate);
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
	export class ProxySaveReq {
	    type: string;
	    host: string;
	    port: number;
	    username: string;
	    password: string;
	    keepExistingPassword: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ProxySaveReq(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.username = source["username"];
	        this.password = source["password"];
	        this.keepExistingPassword = source["keepExistingPassword"];
	    }
	}
	export class ProxyView {
	    type: string;
	    host: string;
	    port: number;
	    username: string;
	    hasPassword: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ProxyView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.username = source["username"];
	        this.hasPassword = source["hasPassword"];
	    }
	}
	export class RemoteFileContent {
	    path: string;
	    content: string;
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new RemoteFileContent(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.content = source["content"];
	        this.size = source["size"];
	    }
	}
	export class SFTPFileEntry {
	    name: string;
	    path: string;
	    isDir: boolean;
	    isSymlink: boolean;
	    size: number;
	    mode: string;
	    // Go type: time
	    modTime: any;
	
	    static createFrom(source: any = {}) {
	        return new SFTPFileEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.isDir = source["isDir"];
	        this.isSymlink = source["isSymlink"];
	        this.size = source["size"];
	        this.mode = source["mode"];
	        this.modTime = this.convertValues(source["modTime"], null);
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
	export class SFTPListResult {
	    path: string;
	    parent: string;
	    entries: SFTPFileEntry[];
	
	    static createFrom(source: any = {}) {
	        return new SFTPListResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.parent = source["parent"];
	        this.entries = this.convertValues(source["entries"], SFTPFileEntry);
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
	export class SSHKeyMeta {
	    id: string;
	    name: string;
	    keyType: string;
	    fingerprint: string;
	    publicKey: string;
	    hasPassphrase: boolean;
	    comment?: string;
	    usageCount: number;
	    // Go type: time
	    createdAt: any;
	
	    static createFrom(source: any = {}) {
	        return new SSHKeyMeta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.keyType = source["keyType"];
	        this.fingerprint = source["fingerprint"];
	        this.publicKey = source["publicKey"];
	        this.hasPassphrase = source["hasPassphrase"];
	        this.comment = source["comment"];
	        this.usageCount = source["usageCount"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
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
	export class ServerSaveRequest {
	    id: string;
	    name: string;
	    group: string;
	    color: string;
	    host: string;
	    port: number;
	    username: string;
	    authType: string;
	    password: string;
	    keepExistingPassword: boolean;
	    sshKeyId: string;
	    proxy: ProxySaveReq;
	    jumpHost: JumpSaveReq;
	    timeoutSeconds: number;
	    keepAliveSeconds: number;
	    defaultRemotePath: string;
	    startupCommand: string;
	    notes: string;
	
	    static createFrom(source: any = {}) {
	        return new ServerSaveRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.group = source["group"];
	        this.color = source["color"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.username = source["username"];
	        this.authType = source["authType"];
	        this.password = source["password"];
	        this.keepExistingPassword = source["keepExistingPassword"];
	        this.sshKeyId = source["sshKeyId"];
	        this.proxy = this.convertValues(source["proxy"], ProxySaveReq);
	        this.jumpHost = this.convertValues(source["jumpHost"], JumpSaveReq);
	        this.timeoutSeconds = source["timeoutSeconds"];
	        this.keepAliveSeconds = source["keepAliveSeconds"];
	        this.defaultRemotePath = source["defaultRemotePath"];
	        this.startupCommand = source["startupCommand"];
	        this.notes = source["notes"];
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
	export class ServerView {
	    id: string;
	    name: string;
	    group: string;
	    color: string;
	    host: string;
	    port: number;
	    username: string;
	    authType: string;
	    hasPassword: boolean;
	    sshKeyId?: string;
	    proxy: ProxyView;
	    jumpHost: JumpHostView;
	    timeoutSeconds: number;
	    keepAliveSeconds: number;
	    defaultRemotePath: string;
	    startupCommand: string;
	    notes: string;
	    // Go type: time
	    createdAt: any;
	    // Go type: time
	    updatedAt: any;
	    // Go type: time
	    lastConnectedAt?: any;
	
	    static createFrom(source: any = {}) {
	        return new ServerView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.group = source["group"];
	        this.color = source["color"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.username = source["username"];
	        this.authType = source["authType"];
	        this.hasPassword = source["hasPassword"];
	        this.sshKeyId = source["sshKeyId"];
	        this.proxy = this.convertValues(source["proxy"], ProxyView);
	        this.jumpHost = this.convertValues(source["jumpHost"], JumpHostView);
	        this.timeoutSeconds = source["timeoutSeconds"];
	        this.keepAliveSeconds = source["keepAliveSeconds"];
	        this.defaultRemotePath = source["defaultRemotePath"];
	        this.startupCommand = source["startupCommand"];
	        this.notes = source["notes"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
	        this.updatedAt = this.convertValues(source["updatedAt"], null);
	        this.lastConnectedAt = this.convertValues(source["lastConnectedAt"], null);
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
	export class SessionInfo {
	    sessionId: string;
	    serverId: string;
	    serverName: string;
	    host: string;
	    port: number;
	    username: string;
	    status: string;
	    sftpReady: boolean;
	    homeDir: string;
	    error?: string;
	    viaSummary?: string;
	    // Go type: time
	    createdAt: any;
	
	    static createFrom(source: any = {}) {
	        return new SessionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sessionId = source["sessionId"];
	        this.serverId = source["serverId"];
	        this.serverName = source["serverName"];
	        this.host = source["host"];
	        this.port = source["port"];
	        this.username = source["username"];
	        this.status = source["status"];
	        this.sftpReady = source["sftpReady"];
	        this.homeDir = source["homeDir"];
	        this.error = source["error"];
	        this.viaSummary = source["viaSummary"];
	        this.createdAt = this.convertValues(source["createdAt"], null);
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
	export class SyncImportSummary {
	    mode: string;
	    serversAdded: number;
	    serversUpdated: number;
	    keysAdded: number;
	    keysUpdated: number;
	    shortcutsAdded: number;
	    shortcutsUpdated: number;
	    templatesAdded: number;
	    templatesUpdated: number;
	
	    static createFrom(source: any = {}) {
	        return new SyncImportSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.serversAdded = source["serversAdded"];
	        this.serversUpdated = source["serversUpdated"];
	        this.keysAdded = source["keysAdded"];
	        this.keysUpdated = source["keysUpdated"];
	        this.shortcutsAdded = source["shortcutsAdded"];
	        this.shortcutsUpdated = source["shortcutsUpdated"];
	        this.templatesAdded = source["templatesAdded"];
	        this.templatesUpdated = source["templatesUpdated"];
	    }
	}
	export class SyncReceivePreview {
	    previewId: string;
	    schemaVersion: number;
	    exportedAt: string;
	    sourcePlatform: string;
	    sourceDevice: string;
	    serverCount: number;
	    keyCount: number;
	    shortcutCount: number;
	    templateCount: number;
	
	    static createFrom(source: any = {}) {
	        return new SyncReceivePreview(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.previewId = source["previewId"];
	        this.schemaVersion = source["schemaVersion"];
	        this.exportedAt = source["exportedAt"];
	        this.sourcePlatform = source["sourcePlatform"];
	        this.sourceDevice = source["sourceDevice"];
	        this.serverCount = source["serverCount"];
	        this.keyCount = source["keyCount"];
	        this.shortcutCount = source["shortcutCount"];
	        this.templateCount = source["templateCount"];
	    }
	}
	export class SyncSendSession {
	    code: string;
	    qrText: string;
	    expiresAt: string;
	    relayUrl: string;
	    serverCount: number;
	    keyCount: number;
	    shortcutCount: number;
	    templateCount: number;
	
	    static createFrom(source: any = {}) {
	        return new SyncSendSession(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.code = source["code"];
	        this.qrText = source["qrText"];
	        this.expiresAt = source["expiresAt"];
	        this.relayUrl = source["relayUrl"];
	        this.serverCount = source["serverCount"];
	        this.keyCount = source["keyCount"];
	        this.shortcutCount = source["shortcutCount"];
	        this.templateCount = source["templateCount"];
	    }
	}
	export class UpdateCheckResult {
	    currentVersion: string;
	    latestVersion: string;
	    hasUpdate: boolean;
	    releaseUrl: string;
	    downloadUrl: string;
	    releaseNotes: string;
	    publishedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateCheckResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.currentVersion = source["currentVersion"];
	        this.latestVersion = source["latestVersion"];
	        this.hasUpdate = source["hasUpdate"];
	        this.releaseUrl = source["releaseUrl"];
	        this.downloadUrl = source["downloadUrl"];
	        this.releaseNotes = source["releaseNotes"];
	        this.publishedAt = source["publishedAt"];
	    }
	}
	export class VaultLockState {
	    locked: boolean;
	    hasPassword: boolean;
	    vaultPath: string;
	    schemaVersion: number;
	
	    static createFrom(source: any = {}) {
	        return new VaultLockState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.locked = source["locked"];
	        this.hasPassword = source["hasPassword"];
	        this.vaultPath = source["vaultPath"];
	        this.schemaVersion = source["schemaVersion"];
	    }
	}
	export class VaultSecurityInfo {
	    encrypted: boolean;
	    engine: string;
	    machineBound: boolean;
	    userBound: boolean;
	    vaultPath: string;
	    keyCount: number;
	    shortcutCount: number;
	    serverCount: number;
	
	    static createFrom(source: any = {}) {
	        return new VaultSecurityInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.encrypted = source["encrypted"];
	        this.engine = source["engine"];
	        this.machineBound = source["machineBound"];
	        this.userBound = source["userBound"];
	        this.vaultPath = source["vaultPath"];
	        this.keyCount = source["keyCount"];
	        this.shortcutCount = source["shortcutCount"];
	        this.serverCount = source["serverCount"];
	    }
	}

}

export namespace sshclient {
	
	export class SSHCacheClearResult {
	    removedKnownHosts: number;
	    clearedDirCaches: number;
	    knownHostsPath: string;
	
	    static createFrom(source: any = {}) {
	        return new SSHCacheClearResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.removedKnownHosts = source["removedKnownHosts"];
	        this.clearedDirCaches = source["clearedDirCaches"];
	        this.knownHostsPath = source["knownHostsPath"];
	    }
	}
	export class SSHCacheInfo {
	    knownHostsPath: string;
	    knownHostCount: number;
	    dirCacheCount: number;
	
	    static createFrom(source: any = {}) {
	        return new SSHCacheInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.knownHostsPath = source["knownHostsPath"];
	        this.knownHostCount = source["knownHostCount"];
	        this.dirCacheCount = source["dirCacheCount"];
	    }
	}

}


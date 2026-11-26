export namespace core {
	
	export class AfterUpload {
	    move: boolean;
	    folder: string;
	    folderOdd: string;
	    folderEven: string;
	    folderVideo: string;
	
	    static createFrom(source: any = {}) {
	        return new AfterUpload(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.move = source["move"];
	        this.folder = source["folder"];
	        this.folderOdd = source["folderOdd"];
	        this.folderEven = source["folderEven"];
	        this.folderVideo = source["folderVideo"];
	    }
	}
	export class Compress {
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new Compress(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.size = source["size"];
	    }
	}
	export class Counts {
	    total: number;
	    pending: number;
	    uploading: number;
	    done: number;
	    failed: number;
	    duplicate: number;
	
	    static createFrom(source: any = {}) {
	        return new Counts(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.total = source["total"];
	        this.pending = source["pending"];
	        this.uploading = source["uploading"];
	        this.done = source["done"];
	        this.failed = source["failed"];
	        this.duplicate = source["duplicate"];
	    }
	}
	export class Location {
	    name: string;
	    latitude: number;
	    longitude: number;
	
	    static createFrom(source: any = {}) {
	        return new Location(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.latitude = source["latitude"];
	        this.longitude = source["longitude"];
	    }
	}
	export class Tree {
	    id: string;
	    tag_id: string;
	    name: string;
	    location: Location;
	    is_bib: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Tree(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.tag_id = source["tag_id"];
	        this.name = source["name"];
	        this.location = this.convertValues(source["location"], Location);
	        this.is_bib = source["is_bib"];
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
	export class Event {
	    name: string;
	    price: number;
	    videoPrice: number;
	    location: string;
	    tags: string;
	    tree?: Tree;
	    description: string;
	    nicknames: string[];
	
	    static createFrom(source: any = {}) {
	        return new Event(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.price = source["price"];
	        this.videoPrice = source["videoPrice"];
	        this.location = source["location"];
	        this.tags = source["tags"];
	        this.tree = this.convertValues(source["tree"], Tree);
	        this.description = source["description"];
	        this.nicknames = source["nicknames"];
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
	export class FTP {
	    enabled: boolean;
	    port: number;
	    user: string;
	
	    static createFrom(source: any = {}) {
	        return new FTP(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.port = source["port"];
	        this.user = source["user"];
	    }
	}
	export class FolderStat {
	    folder: string;
	    done: number;
	    pending: number;
	    failed: number;
	    total: number;
	    pct: number;
	
	    static createFrom(source: any = {}) {
	        return new FolderStat(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.folder = source["folder"];
	        this.done = source["done"];
	        this.pending = source["pending"];
	        this.failed = source["failed"];
	        this.total = source["total"];
	        this.pct = source["pct"];
	    }
	}
	export class Hires {
	    folders: string[];
	    auto: boolean;
	    lut: string;
	    lutStrength: number;
	    autoTone: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Hires(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.folders = source["folders"];
	        this.auto = source["auto"];
	        this.lut = source["lut"];
	        this.lutStrength = source["lutStrength"];
	        this.autoTone = source["autoTone"];
	    }
	}
	export class HiresMatch {
	    type: string;
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new HiresMatch(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.path = source["path"];
	    }
	}
	export class HiresItem {
	    id: string;
	    title: string;
	    kind: string;
	    signature: string;
	    match: HiresMatch;
	    sent: boolean;
	    error: string;
	
	    static createFrom(source: any = {}) {
	        return new HiresItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.title = source["title"];
	        this.kind = source["kind"];
	        this.signature = source["signature"];
	        this.match = this.convertValues(source["match"], HiresMatch);
	        this.sent = source["sent"];
	        this.error = source["error"];
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
	
	export class HiresState {
	    items: HiresItem[];
	    exact: number;
	    byName: number;
	    none: number;
	    sent: number;
	    loading: boolean;
	    error: string;
	    loadedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new HiresState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.items = this.convertValues(source["items"], HiresItem);
	        this.exact = source["exact"];
	        this.byName = source["byName"];
	        this.none = source["none"];
	        this.sent = source["sent"];
	        this.loading = source["loading"];
	        this.error = source["error"];
	        this.loadedAt = source["loadedAt"];
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
	export class LUT {
	    enabled: boolean;
	    file: string;
	    photo: string;
	    video: string;
	    strength: number;
	
	    static createFrom(source: any = {}) {
	        return new LUT(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.file = source["file"];
	        this.photo = source["photo"];
	        this.video = source["video"];
	        this.strength = source["strength"];
	    }
	}
	export class LUTFile {
	    name: string;
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new LUTFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	    }
	}
	
	export class LogLine {
	    t: number;
	    level: string;
	    msg: string;
	
	    static createFrom(source: any = {}) {
	        return new LogLine(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.t = source["t"];
	        this.level = source["level"];
	        this.msg = source["msg"];
	    }
	}
	export class Parity {
	    mode: string;
	    only: boolean;
	
	    static createFrom(source: any = {}) {
	        return new Parity(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mode = source["mode"];
	        this.only = source["only"];
	    }
	}
	export class Preset {
	    name: string;
	    event: Event;
	    compress: Compress;
	
	    static createFrom(source: any = {}) {
	        return new Preset(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.event = this.convertValues(source["event"], Event);
	        this.compress = this.convertValues(source["compress"], Compress);
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
	export class RankRow {
	    id: string;
	    username: string;
	    photo: string;
	    verified: boolean;
	    official: boolean;
	    count: number;
	    videos: number;
	    last: number;
	
	    static createFrom(source: any = {}) {
	        return new RankRow(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.username = source["username"];
	        this.photo = source["photo"];
	        this.verified = source["verified"];
	        this.official = source["official"];
	        this.count = source["count"];
	        this.videos = source["videos"];
	        this.last = source["last"];
	    }
	}
	export class RankView {
	    treeId: string;
	    name: string;
	    range: string;
	    scanned: number;
	    total: number;
	    done: boolean;
	    error: string;
	    updatedAt: number;
	    cached: boolean;
	    rows: RankRow[];
	    accounts: number;
	
	    static createFrom(source: any = {}) {
	        return new RankView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.treeId = source["treeId"];
	        this.name = source["name"];
	        this.range = source["range"];
	        this.scanned = source["scanned"];
	        this.total = source["total"];
	        this.done = source["done"];
	        this.error = source["error"];
	        this.updatedAt = source["updatedAt"];
	        this.cached = source["cached"];
	        this.rows = this.convertValues(source["rows"], RankRow);
	        this.accounts = source["accounts"];
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
	export class ScanSummary {
	    folder: string;
	    media: number;
	    known: number;
	    waiting: number;
	    raw: number;
	    other: number;
	    subdirs: number;
	
	    static createFrom(source: any = {}) {
	        return new ScanSummary(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.folder = source["folder"];
	        this.media = source["media"];
	        this.known = source["known"];
	        this.waiting = source["waiting"];
	        this.raw = source["raw"];
	        this.other = source["other"];
	        this.subdirs = source["subdirs"];
	    }
	}
	export class UsbCam {
	    enabled: boolean;
	    autoSource: boolean;
	
	    static createFrom(source: any = {}) {
	        return new UsbCam(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.autoSource = source["autoSource"];
	    }
	}
	export class Watermark {
	    enabled: boolean;
	    type: string;
	    text: string;
	    pos: string;
	    size: number;
	    opacity: number;
	    logoPath: string;
	
	    static createFrom(source: any = {}) {
	        return new Watermark(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.type = source["type"];
	        this.text = source["text"];
	        this.pos = source["pos"];
	        this.size = source["size"];
	        this.opacity = source["opacity"];
	        this.logoPath = source["logoPath"];
	    }
	}
	export class VideoCfg {
	    enabled: boolean;
	    orientation: string;
	    confirm: boolean;
	    rotateOriginal: boolean;
	
	    static createFrom(source: any = {}) {
	        return new VideoCfg(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.orientation = source["orientation"];
	        this.confirm = source["confirm"];
	        this.rotateOriginal = source["rotateOriginal"];
	    }
	}
	export class Settings {
	    watchFolder: string;
	    watchFolders: string[];
	    includeSubfolders: boolean;
	    destination: string;
	    localOutputFolder: string;
	    maxRetries: number;
	    performance: string;
	    compress: Compress;
	    video: VideoCfg;
	    event: Event;
	    autoUpload: boolean;
	    uploadOnly: boolean;
	    uploadOrder: string;
	    theme: string;
	    lut: LUT;
	    ftp: FTP;
	    afterUpload: AfterUpload;
	    parity: Parity;
	    hires: Hires;
	    watermark: Watermark;
	    presets: Preset[];
	    usbCam: UsbCam;
	    debug: boolean;
	    scanIntervalSec: number;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.watchFolder = source["watchFolder"];
	        this.watchFolders = source["watchFolders"];
	        this.includeSubfolders = source["includeSubfolders"];
	        this.destination = source["destination"];
	        this.localOutputFolder = source["localOutputFolder"];
	        this.maxRetries = source["maxRetries"];
	        this.performance = source["performance"];
	        this.compress = this.convertValues(source["compress"], Compress);
	        this.video = this.convertValues(source["video"], VideoCfg);
	        this.event = this.convertValues(source["event"], Event);
	        this.autoUpload = source["autoUpload"];
	        this.uploadOnly = source["uploadOnly"];
	        this.uploadOrder = source["uploadOrder"];
	        this.theme = source["theme"];
	        this.lut = this.convertValues(source["lut"], LUT);
	        this.ftp = this.convertValues(source["ftp"], FTP);
	        this.afterUpload = this.convertValues(source["afterUpload"], AfterUpload);
	        this.parity = this.convertValues(source["parity"], Parity);
	        this.hires = this.convertValues(source["hires"], Hires);
	        this.watermark = this.convertValues(source["watermark"], Watermark);
	        this.presets = this.convertValues(source["presets"], Preset);
	        this.usbCam = this.convertValues(source["usbCam"], UsbCam);
	        this.debug = source["debug"];
	        this.scanIntervalSec = source["scanIntervalSec"];
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
	export class Speed {
	    mbps: number;
	    perMin: number;
	    etaSec: number;
	    elapsedSec: number;
	
	    static createFrom(source: any = {}) {
	        return new Speed(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.mbps = source["mbps"];
	        this.perMin = source["perMin"];
	        this.etaSec = source["etaSec"];
	        this.elapsedSec = source["elapsedSec"];
	    }
	}
	export class Status {
	    app: string;
	    paused: boolean;
	    pauseReason: string;
	    pauseMsg: string;
	    counts: Counts;
	    scan: ScanSummary;
	    account: fotoyu.SessionInfo;
	    perMinute: number;
	    destination: string;
	    event: Event;
	    performance: string;
	    speed: Speed;
	    folders: FolderStat[];
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.app = source["app"];
	        this.paused = source["paused"];
	        this.pauseReason = source["pauseReason"];
	        this.pauseMsg = source["pauseMsg"];
	        this.counts = this.convertValues(source["counts"], Counts);
	        this.scan = this.convertValues(source["scan"], ScanSummary);
	        this.account = this.convertValues(source["account"], fotoyu.SessionInfo);
	        this.perMinute = source["perMinute"];
	        this.destination = source["destination"];
	        this.event = this.convertValues(source["event"], Event);
	        this.performance = source["performance"];
	        this.speed = this.convertValues(source["speed"], Speed);
	        this.folders = this.convertValues(source["folders"], FolderStat);
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

export namespace fotoyu {
	
	export class AccountInfo {
	    loggedIn: boolean;
	    username: string;
	    name: string;
	    photo: string;
	    id: string;
	    active: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AccountInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.loggedIn = source["loggedIn"];
	        this.username = source["username"];
	        this.name = source["name"];
	        this.photo = source["photo"];
	        this.id = source["id"];
	        this.active = source["active"];
	    }
	}
	export class FeeItem {
	    label: string;
	    nominal: number;
	    percentage: number;
	
	    static createFrom(source: any = {}) {
	        return new FeeItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.nominal = source["nominal"];
	        this.percentage = source["percentage"];
	    }
	}
	export class FeeResult {
	    buyer: number;
	    net: number;
	    netPct: number;
	    items: FeeItem[];
	
	    static createFrom(source: any = {}) {
	        return new FeeResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.buyer = source["buyer"];
	        this.net = source["net"];
	        this.netPct = source["netPct"];
	        this.items = this.convertValues(source["items"], FeeItem);
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
	export class Location {
	    name: string;
	    latitude: number;
	    longitude: number;
	
	    static createFrom(source: any = {}) {
	        return new Location(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.latitude = source["latitude"];
	        this.longitude = source["longitude"];
	    }
	}
	export class SessionInfo {
	    loggedIn: boolean;
	    username: string;
	    name: string;
	    photo: string;
	    id: string;
	
	    static createFrom(source: any = {}) {
	        return new SessionInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.loggedIn = source["loggedIn"];
	        this.username = source["username"];
	        this.name = source["name"];
	        this.photo = source["photo"];
	        this.id = source["id"];
	    }
	}
	export class StorageInfo {
	    used: number;
	    remaining: number;
	    limit: number;
	    files: number;
	
	    static createFrom(source: any = {}) {
	        return new StorageInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.used = source["used"];
	        this.remaining = source["remaining"];
	        this.limit = source["limit"];
	        this.files = source["files"];
	    }
	}
	export class TreeResult {
	    id: string;
	    tag_id: string;
	    name: string;
	    alias: string;
	    category: string;
	    location: Location;
	    is_bib: boolean;
	    image: string;
	    official: boolean;
	    count?: number;
	    host: string;
	
	    static createFrom(source: any = {}) {
	        return new TreeResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.tag_id = source["tag_id"];
	        this.name = source["name"];
	        this.alias = source["alias"];
	        this.category = source["category"];
	        this.location = this.convertValues(source["location"], Location);
	        this.is_bib = source["is_bib"];
	        this.image = source["image"];
	        this.official = source["official"];
	        this.count = source["count"];
	        this.host = source["host"];
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
	
	export class FTPInfo {
	    enabled: boolean;
	    running: boolean;
	    port: number;
	    error: string;
	    user: string;
	    addresses: string[];
	    connected: number;
	    files: number;
	    bytes: number;
	    lastFile: string;
	    lastAt: number;
	    pass: string;
	
	    static createFrom(source: any = {}) {
	        return new FTPInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.enabled = source["enabled"];
	        this.running = source["running"];
	        this.port = source["port"];
	        this.error = source["error"];
	        this.user = source["user"];
	        this.addresses = source["addresses"];
	        this.connected = source["connected"];
	        this.files = source["files"];
	        this.bytes = source["bytes"];
	        this.lastFile = source["lastFile"];
	        this.lastAt = source["lastAt"];
	        this.pass = source["pass"];
	    }
	}
	export class VFItem {
	    video: string;
	    dir: string;
	    files: string[];
	    enlarged: number;
	    similar: number;
	    asked: number;
	    skipped: boolean;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new VFItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.video = source["video"];
	        this.dir = source["dir"];
	        this.files = source["files"];
	        this.enlarged = source["enlarged"];
	        this.similar = source["similar"];
	        this.asked = source["asked"];
	        this.skipped = source["skipped"];
	        this.error = source["error"];
	    }
	}
	export class VFState {
	    running: boolean;
	    cancelling: boolean;
	    total: number;
	    index: number;
	    current: string;
	    stage: string;
	    items: VFItem[];
	    photos: number;
	    queued: number;
	    paused: boolean;
	    available: boolean;
	
	    static createFrom(source: any = {}) {
	        return new VFState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.running = source["running"];
	        this.cancelling = source["cancelling"];
	        this.total = source["total"];
	        this.index = source["index"];
	        this.current = source["current"];
	        this.stage = source["stage"];
	        this.items = this.convertValues(source["items"], VFItem);
	        this.photos = source["photos"];
	        this.queued = source["queued"];
	        this.paused = source["paused"];
	        this.available = source["available"];
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

export namespace usbcam {
	
	export class Drive {
	    path: string;
	    root: string;
	    label: string;
	    files: number;
	
	    static createFrom(source: any = {}) {
	        return new Drive(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.root = source["root"];
	        this.label = source["label"];
	        this.files = source["files"];
	    }
	}
	export class State {
	    available: boolean;
	    running: boolean;
	    connected: boolean;
	    model: string;
	    files: number;
	    ignored: number;
	    error: string;
	    dir: string;
	
	    static createFrom(source: any = {}) {
	        return new State(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.running = source["running"];
	        this.connected = source["connected"];
	        this.model = source["model"];
	        this.files = source["files"];
	        this.ignored = source["ignored"];
	        this.error = source["error"];
	        this.dir = source["dir"];
	    }
	}

}


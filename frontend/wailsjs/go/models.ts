export namespace main {
	
	export class AlbumEntry {
	    id: string;
	    kind: string;
	    mode: string;
	    timestamp: string;
	    displayTime: string;
	    filename: string;
	    status: string;
	    path: string;
	
	    static createFrom(source: any = {}) {
	        return new AlbumEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.mode = source["mode"];
	        this.timestamp = source["timestamp"];
	        this.displayTime = source["displayTime"];
	        this.filename = source["filename"];
	        this.status = source["status"];
	        this.path = source["path"];
	    }
	}
	export class AudioDeviceInfo {
	    id: string;
	    name: string;
	    isDefault: boolean;
	
	    static createFrom(source: any = {}) {
	        return new AudioDeviceInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.isDefault = source["isDefault"];
	    }
	}
	export class AudioDeviceLists {
	    inputs: AudioDeviceInfo[];
	    outputs: AudioDeviceInfo[];
	
	    static createFrom(source: any = {}) {
	        return new AudioDeviceLists(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.inputs = this.convertValues(source["inputs"], AudioDeviceInfo);
	        this.outputs = this.convertValues(source["outputs"], AudioDeviceInfo);
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
	export class AudioSettings {
	    sampleRate: number;
	    captureDeviceId: string;
	    playbackDeviceId: string;
	    pttMode: string;
	    rigctlAddress: string;
	    serialPort: string;
	    activeHigh: boolean;
	    pttLeadMs: number;
	    pttTailMs: number;
	
	    static createFrom(source: any = {}) {
	        return new AudioSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.sampleRate = source["sampleRate"];
	        this.captureDeviceId = source["captureDeviceId"];
	        this.playbackDeviceId = source["playbackDeviceId"];
	        this.pttMode = source["pttMode"];
	        this.rigctlAddress = source["rigctlAddress"];
	        this.serialPort = source["serialPort"];
	        this.activeHigh = source["activeHigh"];
	        this.pttLeadMs = source["pttLeadMs"];
	        this.pttTailMs = source["pttTailMs"];
	    }
	}
	export class AudioState {
	    connected: boolean;
	    rxActive: boolean;
	    txActive: boolean;
	    sampleRate: number;
	    pttMode: string;
	
	    static createFrom(source: any = {}) {
	        return new AudioState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.connected = source["connected"];
	        this.rxActive = source["rxActive"];
	        this.txActive = source["txActive"];
	        this.sampleRate = source["sampleRate"];
	        this.pttMode = source["pttMode"];
	    }
	}
	export class ImageLibraryEntry {
	    id: string;
	    name: string;
	    width: number;
	    height: number;
	    size: number;
	    addedAt: string;
	    thumbnail: string;
	
	    static createFrom(source: any = {}) {
	        return new ImageLibraryEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.size = source["size"];
	        this.addedAt = source["addedAt"];
	        this.thumbnail = source["thumbnail"];
	    }
	}
	export class Mode {
	    id: string;
	    name: string;
	    width: number;
	    height: number;
	    ready: boolean;
	    description: string;
	
	    static createFrom(source: any = {}) {
	        return new Mode(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.ready = source["ready"];
	        this.description = source["description"];
	    }
	}
	export class StationSettings {
	    myCall: string;
	    locator: string;
	
	    static createFrom(source: any = {}) {
	        return new StationSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.myCall = source["myCall"];
	        this.locator = source["locator"];
	    }
	}
	export class TemplateObject {
	    type: string;
	    text?: string;
	    imageData?: string;
	    imageName?: string;
	    x: number;
	    y: number;
	    width?: number;
	    height?: number;
	    fontSize?: number;
	    color?: string;
	    bold?: boolean;
	    outlineEnabled?: boolean;
	    outlineWidth?: number;
	    outlineColor?: string;
	
	    static createFrom(source: any = {}) {
	        return new TemplateObject(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.type = source["type"];
	        this.text = source["text"];
	        this.imageData = source["imageData"];
	        this.imageName = source["imageName"];
	        this.x = source["x"];
	        this.y = source["y"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.fontSize = source["fontSize"];
	        this.color = source["color"];
	        this.bold = source["bold"];
	        this.outlineEnabled = source["outlineEnabled"];
	        this.outlineWidth = source["outlineWidth"];
	        this.outlineColor = source["outlineColor"];
	    }
	}
	export class TemplateSlot {
	    id: string;
	    name: string;
	    objects: TemplateObject[];
	
	    static createFrom(source: any = {}) {
	        return new TemplateSlot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.objects = this.convertValues(source["objects"], TemplateObject);
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


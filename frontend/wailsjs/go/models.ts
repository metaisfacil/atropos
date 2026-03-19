export namespace image {
	
	export class Point {
	    X: number;
	    Y: number;
	
	    static createFrom(source: any = {}) {
	        return new Point(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.X = source["X"];
	        this.Y = source["Y"];
	    }
	}

}

export namespace main {
	
	export class ClickCornerRequest {
	    x: number;
	    y: number;
	    custom: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ClickCornerRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.x = source["x"];
	        this.y = source["y"];
	        this.custom = source["custom"];
	    }
	}
	export class ClickCornerResult {
	    preview: string;
	    message: string;
	    count: number;
	    done: boolean;
	    snappedX: number;
	    snappedY: number;
	    width: number;
	    height: number;
	
	    static createFrom(source: any = {}) {
	        return new ClickCornerResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.preview = source["preview"];
	        this.message = source["message"];
	        this.count = source["count"];
	        this.done = source["done"];
	        this.snappedX = source["snappedX"];
	        this.snappedY = source["snappedY"];
	        this.width = source["width"];
	        this.height = source["height"];
	    }
	}
	export class CompositorResult {
	    preview: string;
	    width: number;
	    height: number;
	    message: string;
	
	    static createFrom(source: any = {}) {
	        return new CompositorResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.preview = source["preview"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.message = source["message"];
	    }
	}
	export class CompositorSaveRequest {
	    outputPath: string;
	
	    static createFrom(source: any = {}) {
	        return new CompositorSaveRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outputPath = source["outputPath"];
	    }
	}
	export class CompositorStitchRequest {
	    imagePaths: string[];
	    orientation: string;
	
	    static createFrom(source: any = {}) {
	        return new CompositorStitchRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.imagePaths = source["imagePaths"];
	        this.orientation = source["orientation"];
	    }
	}
	export class CornerDetectRequest {
	    maxCorners: number;
	    qualityLevel: number;
	    minDistance: number;
	    accentValue: number;
	    useStretch: boolean;
	    stretchLow: number;
	    stretchHigh: number;
	
	    static createFrom(source: any = {}) {
	        return new CornerDetectRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.maxCorners = source["maxCorners"];
	        this.qualityLevel = source["qualityLevel"];
	        this.minDistance = source["minDistance"];
	        this.accentValue = source["accentValue"];
	        this.useStretch = source["useStretch"];
	        this.stretchLow = source["stretchLow"];
	        this.stretchHigh = source["stretchHigh"];
	    }
	}
	export class CropRequest {
	    direction: string;
	
	    static createFrom(source: any = {}) {
	        return new CropRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.direction = source["direction"];
	    }
	}
	export class DiscDrawRequest {
	    centerX: number;
	    centerY: number;
	    radius: number;
	
	    static createFrom(source: any = {}) {
	        return new DiscDrawRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.centerX = source["centerX"];
	        this.centerY = source["centerY"];
	        this.radius = source["radius"];
	    }
	}
	export class DiscRotateRequest {
	    angle: number;
	
	    static createFrom(source: any = {}) {
	        return new DiscRotateRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.angle = source["angle"];
	    }
	}
	export class DiscSettings {
	    centerCutout: boolean;
	    cutoutPercent: number;
	
	    static createFrom(source: any = {}) {
	        return new DiscSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.centerCutout = source["centerCutout"];
	        this.cutoutPercent = source["cutoutPercent"];
	    }
	}
	export class FeatherSizeRequest {
	    size: number;
	
	    static createFrom(source: any = {}) {
	        return new FeatherSizeRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.size = source["size"];
	    }
	}
	export class SuggestedCornerParams {
	    minDistance: number;
	    maxCorners: number;
	
	    static createFrom(source: any = {}) {
	        return new SuggestedCornerParams(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.minDistance = source["minDistance"];
	        this.maxCorners = source["maxCorners"];
	    }
	}
	export class ImageInfo {
	    width: number;
	    height: number;
	    preview: string;
	    format: string;
	    dpiX: number;
	    dpiY: number;
	    suggestedCornerParams: SuggestedCornerParams;
	
	    static createFrom(source: any = {}) {
	        return new ImageInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.width = source["width"];
	        this.height = source["height"];
	        this.preview = source["preview"];
	        this.format = source["format"];
	        this.dpiX = source["dpiX"];
	        this.dpiY = source["dpiY"];
	        this.suggestedCornerParams = this.convertValues(source["suggestedCornerParams"], SuggestedCornerParams);
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
	export class LaunchArgs {
	    filePath: string;
	    mode: string;
	    postSaveCommand?: string;
	    postSaveEnabled?: boolean;
	    postSaveExit?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new LaunchArgs(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.filePath = source["filePath"];
	        this.mode = source["mode"];
	        this.postSaveCommand = source["postSaveCommand"];
	        this.postSaveEnabled = source["postSaveEnabled"];
	        this.postSaveExit = source["postSaveExit"];
	    }
	}
	export class LineAddRequest {
	    x1: number;
	    y1: number;
	    x2: number;
	    y2: number;
	
	    static createFrom(source: any = {}) {
	        return new LineAddRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.x1 = source["x1"];
	        this.y1 = source["y1"];
	        this.x2 = source["x2"];
	        this.y2 = source["y2"];
	    }
	}
	export class LoadImageRequest {
	    filePath: string;
	
	    static createFrom(source: any = {}) {
	        return new LoadImageRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.filePath = source["filePath"];
	    }
	}
	export class NormalCropRequest {
	    x1: number;
	    y1: number;
	    x2: number;
	    y2: number;
	
	    static createFrom(source: any = {}) {
	        return new NormalCropRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.x1 = source["x1"];
	        this.y1 = source["y1"];
	        this.x2 = source["x2"];
	        this.y2 = source["y2"];
	    }
	}
	export class PixelColorRequest {
	    x: number;
	    y: number;
	
	    static createFrom(source: any = {}) {
	        return new PixelColorRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.x = source["x"];
	        this.y = source["y"];
	    }
	}
	export class ProcessResult {
	    preview: string;
	    message: string;
	    width: number;
	    height: number;
	    black?: number;
	    white?: number;
	    corners?: image.Point[];
	    uncropped?: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ProcessResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.preview = source["preview"];
	        this.message = source["message"];
	        this.width = source["width"];
	        this.height = source["height"];
	        this.black = source["black"];
	        this.white = source["white"];
	        this.corners = this.convertValues(source["corners"], image.Point);
	        this.uncropped = source["uncropped"];
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
	export class RestoreCornerOverlayRequest {
	    dotRadius: number;
	
	    static createFrom(source: any = {}) {
	        return new RestoreCornerOverlayRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dotRadius = source["dotRadius"];
	    }
	}
	export class RotateRequest {
	    flipCode: number;
	
	    static createFrom(source: any = {}) {
	        return new RotateRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.flipCode = source["flipCode"];
	    }
	}
	export class SaveRequest {
	    outputPath: string;
	
	    static createFrom(source: any = {}) {
	        return new SaveRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.outputPath = source["outputPath"];
	    }
	}
	export class SetLevelsRequest {
	    black: number;
	    white: number;
	
	    static createFrom(source: any = {}) {
	        return new SetLevelsRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.black = source["black"];
	        this.white = source["white"];
	    }
	}
	export class ShiftDiscRequest {
	    dx: number;
	    dy: number;
	
	    static createFrom(source: any = {}) {
	        return new ShiftDiscRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dx = source["dx"];
	        this.dy = source["dy"];
	    }
	}
	export class StraightEdgeRotateRequest {
	    angleDeg: number;
	
	    static createFrom(source: any = {}) {
	        return new StraightEdgeRotateRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.angleDeg = source["angleDeg"];
	    }
	}
	
	export class TouchupSettings {
	    backend: string;
	    iopaintUrl: string;
	
	    static createFrom(source: any = {}) {
	        return new TouchupSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.backend = source["backend"];
	        this.iopaintUrl = source["iopaintUrl"];
	    }
	}
	export class WarpSettings {
	    fillMode: string;
	    fillColor: string;
	
	    static createFrom(source: any = {}) {
	        return new WarpSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.fillMode = source["fillMode"];
	        this.fillColor = source["fillColor"];
	    }
	}

}


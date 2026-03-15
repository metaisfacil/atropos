export namespace main {
	
	export class ClickCornerRequest {
	    x: number;
	    y: number;
	    custom: boolean;
	    dotRadius: number;
	
	    static createFrom(source: any = {}) {
	        return new ClickCornerRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.x = source["x"];
	        this.y = source["y"];
	        this.custom = source["custom"];
	        this.dotRadius = source["dotRadius"];
	    }
	}
	export class ClickCornerResult {
	    preview: string;
	    message: string;
	    count: number;
	    done: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ClickCornerResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.preview = source["preview"];
	        this.message = source["message"];
	        this.count = source["count"];
	        this.done = source["done"];
	    }
	}
	export class CornerDetectRequest {
	    maxCorners: number;
	    qualityLevel: number;
	    minDistance: number;
	    accentValue: number;
	    dotRadius: number;
	
	    static createFrom(source: any = {}) {
	        return new CornerDetectRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.maxCorners = source["maxCorners"];
	        this.qualityLevel = source["qualityLevel"];
	        this.minDistance = source["minDistance"];
	        this.accentValue = source["accentValue"];
	        this.dotRadius = source["dotRadius"];
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
	export class ImageInfo {
	    width: number;
	    height: number;
	    preview: string;
	
	    static createFrom(source: any = {}) {
	        return new ImageInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.width = source["width"];
	        this.height = source["height"];
	        this.preview = source["preview"];
	    }
	}
	export class LaunchArgs {
	    filePath: string;
	    mode: string;
	
	    static createFrom(source: any = {}) {
	        return new LaunchArgs(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.filePath = source["filePath"];
	        this.mode = source["mode"];
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
	
	    static createFrom(source: any = {}) {
	        return new ProcessResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.preview = source["preview"];
	        this.message = source["message"];
	        this.width = source["width"];
	        this.height = source["height"];
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

}

export namespace struct { DotRadius int "json:\"dotRadius\"" } {
	
	export class  {
	    dotRadius: number;
	
	    static createFrom(source: any = {}) {
	        return new (source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.dotRadius = source["dotRadius"];
	    }
	}

}


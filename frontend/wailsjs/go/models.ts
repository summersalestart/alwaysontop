export namespace main {
	
	export class WindowInfo {
	    hwnd: number;
	    title: string;
	    pid: number;
	    process_name: string;
	    topmost: boolean;
	
	    static createFrom(source: any = {}) {
	        return new WindowInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.hwnd = source["hwnd"];
	        this.title = source["title"];
	        this.pid = source["pid"];
	        this.process_name = source["process_name"];
	        this.topmost = source["topmost"];
	    }
	}
	export class ListWindowsResult {
	    success: boolean;
	    windows: WindowInfo[];
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new ListWindowsResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.windows = this.convertValues(source["windows"], WindowInfo);
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
	export class SetTopmostResult {
	    success: boolean;
	    hwnd: number;
	    topmost: boolean;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new SetTopmostResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.success = source["success"];
	        this.hwnd = source["hwnd"];
	        this.topmost = source["topmost"];
	        this.error = source["error"];
	    }
	}

}


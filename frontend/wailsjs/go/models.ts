export namespace main {
	
	export class State {
	    version: string;
	    ips: string[];
	    port: number;
	    user: string;
	    pass: string;
	    folder: string;
	    online: boolean;
	    errorBadge: string;
	    errorDetail: string;
	    lastFile: string;
	    lastAtMs: number;
	
	    static createFrom(source: any = {}) {
	        return new State(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.ips = source["ips"];
	        this.port = source["port"];
	        this.user = source["user"];
	        this.pass = source["pass"];
	        this.folder = source["folder"];
	        this.online = source["online"];
	        this.errorBadge = source["errorBadge"];
	        this.errorDetail = source["errorDetail"];
	        this.lastFile = source["lastFile"];
	        this.lastAtMs = source["lastAtMs"];
	    }
	}

}

export namespace options {
	
	export class SecondInstanceData {
	    Args: string[];
	    WorkingDirectory: string;
	
	    static createFrom(source: any = {}) {
	        return new SecondInstanceData(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Args = source["Args"];
	        this.WorkingDirectory = source["WorkingDirectory"];
	    }
	}

}


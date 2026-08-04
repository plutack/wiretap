export namespace gui {
	
	export class CaptureView {
	    id: number;
	    at: string;
	    method?: string;
	    url?: string;
	    status?: number;
	    req_headers?: Record<string, Array<string>>;
	    req_body?: string;
	    req_body_len: number;
	    resp_headers?: Record<string, Array<string>>;
	    resp_body?: string;
	    resp_body_len: number;
	
	    static createFrom(source: any = {}) {
	        return new CaptureView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.at = source["at"];
	        this.method = source["method"];
	        this.url = source["url"];
	        this.status = source["status"];
	        this.req_headers = source["req_headers"];
	        this.req_body = source["req_body"];
	        this.req_body_len = source["req_body_len"];
	        this.resp_headers = source["resp_headers"];
	        this.resp_body = source["resp_body"];
	        this.resp_body_len = source["resp_body_len"];
	    }
	}
	export class ReplayResult {
	    status: number;
	
	    static createFrom(source: any = {}) {
	        return new ReplayResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.status = source["status"];
	    }
	}
	export class ScriptView {
	    id: number;
	    name: string;
	    trigger: string;
	    body: string;
	    priority: number;
	    enabled: boolean;
	    created_at: string;
	    updated_at: string;
	
	    static createFrom(source: any = {}) {
	        return new ScriptView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.trigger = source["trigger"];
	        this.body = source["body"];
	        this.priority = source["priority"];
	        this.enabled = source["enabled"];
	        this.created_at = source["created_at"];
	        this.updated_at = source["updated_at"];
	    }
	}
	export class ScriptInput {
	    id: number;
	    name: string;
	    trigger: string;
	    body: string;
	    priority: number;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ScriptInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.trigger = source["trigger"];
	        this.body = source["body"];
	        this.priority = source["priority"];
	        this.enabled = source["enabled"];
	    }
	}
	export class ScriptTestRequest {
	    body: string;
	    method: string;
	    url: string;
	    headers: Record<string, string>;
	    req_body: string;
	    status: number;
	
	    static createFrom(source: any = {}) {
	        return new ScriptTestRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.body = source["body"];
	        this.method = source["method"];
	        this.url = source["url"];
	        this.headers = source["headers"];
	        this.req_body = source["req_body"];
	        this.status = source["status"];
	    }
	}
	export class ScriptTestView {
	    method: string;
	    url: string;
	    req_headers: Record<string, Array<string>>;
	    req_body: string;
	    status: number;
	    resp_headers: Record<string, Array<string>>;
	    resp_body: string;
	    logs: Array<string>;
	    rejected: boolean;
	    reject_reason?: string;
	    error?: string;
	
	    static createFrom(source: any = {}) {
	        return new ScriptTestView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.method = source["method"];
	        this.url = source["url"];
	        this.req_headers = source["req_headers"];
	        this.req_body = source["req_body"];
	        this.status = source["status"];
	        this.resp_headers = source["resp_headers"];
	        this.resp_body = source["resp_body"];
	        this.logs = source["logs"];
	        this.rejected = source["rejected"];
	        this.reject_reason = source["reject_reason"];
	        this.error = source["error"];
	    }
	}
	export class StatusView {
	    version: string;
	    store_open: boolean;
	    relay_url?: string;
	    tunnel_running: boolean;
	    connected_projects?: Array<string>;
	
	    static createFrom(source: any = {}) {
	        return new StatusView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.store_open = source["store_open"];
	        this.relay_url = source["relay_url"];
	        this.tunnel_running = source["tunnel_running"];
	        this.connected_projects = source["connected_projects"];
	    }
	}
	export class WebhookView {
	    project: string;
	    seq: number;
	    received_at: string;
	    source_ip?: string;
	    method?: string;
	    path?: string;
	    headers?: Record<string, Array<string>>;
	    body?: string;
	    body_len: number;
	
	    static createFrom(source: any = {}) {
	        return new WebhookView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.project = source["project"];
	        this.seq = source["seq"];
	        this.received_at = source["received_at"];
	        this.source_ip = source["source_ip"];
	        this.method = source["method"];
	        this.path = source["path"];
	        this.headers = source["headers"];
	        this.body = source["body"];
	        this.body_len = source["body_len"];
	    }
	}

}


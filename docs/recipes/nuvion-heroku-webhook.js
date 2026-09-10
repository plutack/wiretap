// Optional local recipe for the Nuvion Heroku log replay workflow.
// Add this in Wiretap as a new transform with trigger: on_compose.
const envelope = json.parse(request.body || "{}");
const data = envelope && envelope.message && envelope.message.data;

if (!data || !data.body || !data.headers) {
  reject("expected a Heroku log envelope with message.data.body and message.data.headers");
} else {
  let targetPath = "";
  if (data.body.event_category) {
    targetPath = "/webhook/bridge";
  } else if (data.body.type) {
    targetPath = "/webhook/fuse";
  } else {
    reject("could not identify a Bridge or Fuse webhook body");
  }

  if (targetPath) {
    const skip = {
      "host": true,
      "content-length": true,
      "via": true,
      "server": true,
      "cdn-loop": true,
      "cf-connecting-ip": true,
      "cf-ipcountry": true,
      "cf-ray": true,
      "cf-visitor": true,
      "x-forwarded-for": true,
      "x-forwarded-port": true,
      "x-forwarded-proto": true,
      "x-nuvion-original-path": true,
      "x-request-start": true,
      "accept-encoding": true
    };

    Object.keys(request.headers).forEach(function (key) {
      delete request.headers[key];
    });
    Object.keys(data.headers).forEach(function (key) {
      if (!skip[key.toLowerCase()]) {
        request.headers[key] = String(data.headers[key]);
      }
    });

    request.method = "POST";
    request.url = request.url.replace(/\/+$/, "") + targetPath;
    request.headers["Content-Type"] = "application/json";
    request.body = json.stringify(data.body);
  }
}

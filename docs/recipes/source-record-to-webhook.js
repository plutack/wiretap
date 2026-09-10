// Example compose recipe: turn a saved source record into a webhook request.
// Add this as a transform with trigger: on_compose, then enable it.
//
// Example source data:
// {
//   "path": "/hooks/orders",
//   "headers": { "X-Event-Source": "sandbox" },
//   "payload": { "event": "order.created", "id": "evt_123" }
// }
const source = json.parse(request.body || "{}");

if (!source.path || source.path[0] !== "/" || !source.payload) {
  reject("expected path and payload fields");
} else {
  Object.keys(request.headers).forEach(function (key) {
    delete request.headers[key];
  });
  Object.keys(source.headers || {}).forEach(function (key) {
    request.headers[key] = String(source.headers[key]);
  });

  request.method = "POST";
  request.url = request.url.replace(/\/+$/, "") + source.path;
  request.headers["Content-Type"] = "application/json";
  request.body = json.stringify(source.payload);
  console.log("prepared " + source.path);
}

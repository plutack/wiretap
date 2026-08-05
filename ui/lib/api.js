// Thin async wrappers over the Wails-bound methods. Every binding returns a
// Promise resolving to the JSON view from internal/gui. Centralizing the
// imports here keeps components decoupled from the generated wailsjs path and
// gives one place to add error shaping later.
import {
  DeleteScript,
  GetScript,
  GetCapture,
  GetWebhook,
  ListCaptures,
  ListScripts,
  ListWebhooks,
  ReplayWebhook,
  SaveScript,
  SetScriptEnabled,
  Status,
  TestScript,
} from "../wailsjs/go/gui/Bindings.js";

export const api = {
  listWebhooks: (project = "") => ListWebhooks(project),
  getWebhook: (project, seq) => GetWebhook(project, seq),
  replayWebhook: (project, seq, targetURL) => ReplayWebhook(project, seq, targetURL),
  listCaptures: () => ListCaptures(),
  getCapture: (id) => GetCapture(id),
  status: () => Status(),
  listScripts: () => ListScripts(),
  getScript: (id) => GetScript(id),
  saveScript: (input) => SaveScript(input),
  setScriptEnabled: (id, enabled) => SetScriptEnabled(id, enabled),
  deleteScript: (id) => DeleteScript(id),
  testScript: (req) => TestScript(req),
};

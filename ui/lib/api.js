// Thin async wrappers over the Wails-bound methods. Every binding returns a
// Promise resolving to the JSON view from internal/gui. Centralizing the
// imports here keeps components decoupled from the generated bindings path
// (ui/bindings, produced by `wails3 generate bindings -b -noevents -names`)
// and gives one place to add error shaping later.
import {
  DeleteScript,
  ExportCapture,
  ExportTargets,
  ExportWebhook,
  GetScript,
  GetCapture,
  GetCaptureBody,
  GetSettings,
  GetWebhook,
  ListCaptures,
  ListScripts,
  ListSessions,
  ListWebhooks,
  RegisterRelay,
  ReplayWebhook,
  SaveScript,
  SaveSettings,
  SetScriptEnabled,
  Status,
  TestScript,
} from "../bindings/github.com/plutack/wiretap/internal/gui/bindings.js";

export const api = {
  listWebhooks: (project = "") => ListWebhooks(project),
  getWebhook: (project, seq) => GetWebhook(project, seq),
  replayWebhook: (project, seq, targetURL) => ReplayWebhook(project, seq, targetURL),
  listCaptures: (sessionId = 0) => ListCaptures(sessionId),
  listSessions: (beforeID = 0, limit = 20) => ListSessions(beforeID, limit),
  getCapture: (id) => GetCapture(id),
  getCaptureBody: (id, part, limit) => GetCaptureBody(id, part, limit),
  status: () => Status(),
  listScripts: () => ListScripts(),
  getScript: (id) => GetScript(id),
  saveScript: (input) => SaveScript(input),
  setScriptEnabled: (id, enabled) => SetScriptEnabled(id, enabled),
  deleteScript: (id) => DeleteScript(id),
  testScript: (req) => TestScript(req),
  exportTargets: () => ExportTargets(),
  exportCapture: (id, target, client) => ExportCapture(id, target, client),
  exportWebhook: (project, seq, target, client) =>
    ExportWebhook(project, seq, target, client),
  getSettings: () => GetSettings(),
  saveSettings: (input) => SaveSettings(input),
  registerRelay: (input) => RegisterRelay(input),
};

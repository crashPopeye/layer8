import type { Config } from "./types.js";
import { shellQuote } from "./tmux.js";

export type Launch = { file: string; args: string[] };

const appleScriptString = (s: string) => `"${s.replace(/\\/g, "\\\\").replace(/"/g, '\\"')}"`;

function fillTemplate(template: string, vars: Record<string, string>): Launch {
  // Templates are split on spaces before substitution so values with spaces stay one argument.
  const parts = template.trim().split(/\s+/).map((part) =>
    part.replace(/\{(\w+)\}/g, (_, key: string) => vars[key] ?? ""),
  );
  return { file: parts[0], args: parts.slice(1) };
}

/**
 * How to open the user's own terminal app in `cwd` running `cmd` (a shell
 * command line). Each app is started through its own CLI so the user's
 * profile, fonts and keybindings apply.
 */
export function terminalLaunch(cfg: Config, platform: NodeJS.Platform, cwd: string, cmd: string): Launch {
  const mac = platform === "darwin";
  const shell = process.env.SHELL || "/bin/zsh";
  // Login + interactive so rc files load before the command runs.
  const viaShell = [shell, "-lic", cmd];
  switch (cfg.terminal) {
    case "ghostty":
      return mac
        ? { file: "open", args: ["-na", "Ghostty", "--args", `--working-directory=${cwd}`, "-e", ...viaShell] }
        : { file: "ghostty", args: [`--working-directory=${cwd}`, "-e", ...viaShell] };
    case "wezterm":
      return { file: "wezterm", args: ["start", "--cwd", cwd, "--", ...viaShell] };
    case "kitty":
      return mac
        ? { file: "open", args: ["-na", "kitty", "--args", "--directory", cwd, ...viaShell] }
        : { file: "kitty", args: ["--directory", cwd, ...viaShell] };
    case "iterm": {
      const line = `cd ${shellQuote(cwd)} && ${cmd}`;
      const script = [
        'tell application "iTerm"',
        "  activate",
        "  set w to (create window with default profile)",
        `  tell current session of w to write text ${appleScriptString(line)}`,
        "end tell",
      ].join("\n");
      return { file: "osascript", args: ["-e", script] };
    }
    case "terminal": {
      const line = `cd ${shellQuote(cwd)} && ${cmd}`;
      const script = `tell application "Terminal"\n  activate\n  do script ${appleScriptString(line)}\nend tell`;
      return { file: "osascript", args: ["-e", script] };
    }
    case "custom":
      if (!cfg.terminalCommand) throw new Error("terminal is 'custom' but terminalCommand is not set");
      return fillTemplate(cfg.terminalCommand, { cwd, cmd });
  }
}

export function editorLaunch(cfg: Config, path: string): Launch {
  switch (cfg.editor) {
    case "code":
      return { file: "code", args: ["-n", path] };
    case "cursor":
      return { file: "cursor", args: ["-n", path] };
    case "zed":
      return { file: "zed", args: [path] };
    case "idea":
      return { file: "idea", args: [path] };
    case "custom":
      if (!cfg.editorCommand) throw new Error("editor is 'custom' but editorCommand is not set");
      return fillTemplate(cfg.editorCommand, { path });
  }
}

import { spawnSync } from "node:child_process"
import { existsSync } from "node:fs"
import { join } from "node:path"

export const PreAddGoGate = async () => {
  return {
    "tool.execute.before": async (input: { tool: string }, output: { args: { command?: unknown } }) => {
      if (input.tool !== "bash" || typeof output.args.command !== "string") return

      const gate = join(process.cwd(), ".claude", "hooks", "pre-add-go.sh")
      if (!existsSync(gate)) return

      const result = spawnSync(gate, {
        cwd: process.cwd(),
        encoding: "utf8",
        input: JSON.stringify({ tool_input: { command: output.args.command } }),
      })
      if (result.status === 2) {
        throw new Error(result.stderr.trim() || "Go pre-add checks failed.")
      }
    },
  }
}

import os from "os"
import path from "path"

const bootstrap = () => {
  const role = process.env.ATM_ROLE
  const project = process.env.ATM_PROJECT
  if (role !== "developing" || !project) return null

  const atm = process.env.ATM_BIN || "atm"
  const contextFile = process.env.ATM_CONTEXT_FILE || ""
  return `<ATM_DEVELOPING_CONTEXT>
This is an ATM developing session for project ${project}.
Use ATM as the visible work ledger for feature, design, spec, bug, chore, and investigation work.
Use ${atm} for ATM commands.
Before substantial development work, find or create a relevant ATM task and add a short start comment when practical.
Record intentions, progress, decisions, test results, commit references, blockers, and handoff notes as task comments.
Use the atm-developing skill for the full ledger workflow and examples.
Follow repo instructions, existing skills, harness rules, tool permissions, and user directions first; ATM records the work, it does not replace the workflow.
More context: ${contextFile}
To track work, dispatch the atm-manager subagent. The prompt is an optional \`hint: <word>\` line (feature, bug, design, spec, chore, investigation, decision, progress, blocker, handoff, question) followed by a freeform message. Note the reply and continue. Do not branch on it.
</ATM_DEVELOPING_CONTEXT>`
}

const log = async (client, message, extra = {}) => {
  try {
    await client?.app?.log?.({
      body: {
        service: "atm-developing",
        level: "info",
        message,
        extra,
      },
    })
  } catch {
    // Logging must not affect the user's OpenCode session.
  }
}

export const ATMDevelopingPlugin = async ({ client } = {}) => {
  if (process.env.ATM_ROLE === "developing") {
    await log(client, "plugin initialized", {
      role: process.env.ATM_ROLE || "",
      project: process.env.ATM_PROJECT || "",
      run_id: process.env.ATM_RUN_ID || "",
    })
  }

  return {
    config: async (config) => {
      const skillsDir = process.env.ATM_OPENCODE_SKILLS_DIR || path.join(os.homedir(), ".agents", "skills")
      config.skills = config.skills || {}
      config.skills.paths = config.skills.paths || []
      if (!config.skills.paths.includes(skillsDir)) {
        config.skills.paths.push(skillsDir)
      }
    },
    "experimental.chat.messages.transform": async (_input, output) => {
      const context = bootstrap()
      if (!context || !output.messages.length) {
        await log(client, "bootstrap skipped", {
          reason: !context ? "inactive" : "no_messages",
        })
        return
      }
      const firstUser = output.messages.find((m) => m.info.role === "user")
      if (!firstUser || !firstUser.parts.length) {
        await log(client, "bootstrap skipped", {
          reason: !firstUser ? "no_user_message" : "no_user_parts",
        })
        return
      }
      if (firstUser.parts.some((p) => p.type === "text" && p.text.includes("ATM_DEVELOPING_CONTEXT"))) {
        await log(client, "bootstrap skipped", { reason: "already_injected" })
        return
      }
      const ref = firstUser.parts[0]
      firstUser.parts.unshift({ ...ref, type: "text", text: context })
      await log(client, "bootstrap injected", {
        project: process.env.ATM_PROJECT || "",
        run_id: process.env.ATM_RUN_ID || "",
      })
    },
    "shell.env": async (_input, output) => {
      if (process.env.ATM_ROLE !== "developing") return
      for (const key of ["ATM_ROLE", "ATM_PROJECT", "ATM_BIN", "ATM_CONTEXT_FILE", "ATM_ACTOR", "ATM_RUN_ID"]) {
        if (process.env[key]) output.env[key] = process.env[key]
      }
    },
  }
}

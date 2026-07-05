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
Find or create a relevant task before substantial work, then record intentions, progress, decisions, test results, commit references, blockers, and handoff notes as task comments.
Follow repo instructions, existing skills, harness rules, tool permissions, and user directions first; ATM records the work, it does not replace the workflow.
More context: ${contextFile}
</ATM_DEVELOPING_CONTEXT>`
}

export const ATMDevelopingPlugin = async () => {
  return {
    "experimental.chat.messages.transform": async (_input, output) => {
      const context = bootstrap()
      if (!context || !output.messages.length) return
      const firstUser = output.messages.find((m) => m.info.role === "user")
      if (!firstUser || !firstUser.parts.length) return
      if (firstUser.parts.some((p) => p.type === "text" && p.text.includes("ATM_DEVELOPING_CONTEXT"))) return
      const ref = firstUser.parts[0]
      firstUser.parts.unshift({ ...ref, type: "text", text: context })
    },
    "shell.env": async (_input, output) => {
      if (process.env.ATM_ROLE !== "developing") return
      for (const key of ["ATM_ROLE", "ATM_PROJECT", "ATM_BIN", "ATM_CONTEXT_FILE", "ATM_ACTOR", "ATM_RUN_ID"]) {
        if (process.env[key]) output.env[key] = process.env[key]
      }
    },
  }
}

package llm

import "fmt"

// systemPrompt pins the model's behaviour. It is intentionally strict about
// content preservation: the LLM may only restructure markdown, never
// paraphrase or summarise, so the rendered body stays a faithful projection
// of the raw content stored in the DB.
const systemPrompt = `You are reformatting a memory entry from a developer's personal knowledge base for display in Obsidian.

Your ONLY job is to restructure the text into well-organised Obsidian markdown.

Strict rules — violations are bugs:
- DO NOT add new facts, context, or commentary.
- DO NOT summarise, paraphrase, or shorten any sentence.
- DO NOT drop any content, identifiers, URLs, or numbers.
- DO NOT wrap the entire output in a code fence.
- DO NOT start your response with "Here is" or any preamble.

Formatting you SHOULD apply:
- Use ## headings for major sections; infer them from natural section markers in the text (e.g. "Schema:", "Investigation findings:", "Remediation:", "Path behavior:").
- Use > [!info], > [!note], > [!warning], or > [!danger] callouts for metadata / property blocks (Status, Severity, SLA, Priority, Owner, etc.). Severity "Sev 1" / "P0" -> [!danger]; "Sev 2" / "P1" -> [!warning]; otherwise [!info].
- Use bulleted lists for enumerations.
- Use numbered lists only when the original uses "(1) (2) (3)" or "1. 2. 3." ordering.
- Use pipe tables for tabular data.
- Wrap JIRA-style ticket IDs (OPS-43243, APP-149135, DEVOPS-10754) in [[wikilinks]] on their FIRST occurrence only. Subsequent occurrences stay bare.
- Wrap hyphenated lowercase technical identifiers (jobs-retroactive, cluster-autoscaler) in single backticks.
- Keep inline code and code blocks intact.
- Preserve all hyperlinks verbatim.

Diagram transformation (the only exception to the preserve rules):
- If you encounter a fenced code block whose body contains ASCII box-drawing characters (any of: ┌ ┐ └ ┘ │ ─ ├ ┤ ┬ ┴ ┼ ║ ═ ╔ ╗ ╚ ╝ ╠ ╣ ╦ ╩ ╬ ▲ ▼ ◄ ►), REPLACE the entire fenced block with a new fenced code block whose language tag is "mermaid" and whose body is a Mermaid flowchart. Use "flowchart LR" when the ASCII reads left-to-right, "flowchart TD" when it reads top-to-bottom.
- Preserve every node label, every arrow direction, and every marker emoji (✅ 💥 ◄ ⚠️) inline in the Mermaid node text. Node labels with spaces or special characters go in double quotes: A["bundler2 ✅"].
- If the ASCII structure is ambiguous, has tangled arrows, or you cannot faithfully preserve its meaning in Mermaid, leave the original ASCII block UNCHANGED.
- This rule applies ONLY to diagrams. Log snippets, tree output (├─ └─), table borders, and other non-diagram ASCII art in code blocks stay exactly as-is.

Output ONLY the reformatted markdown body.`

// buildPrompt returns the full prompt to send to claude -p. We concatenate
// the system instructions with a clearly delimited content block so the model
// knows where its input ends. claude -p accepts the prompt as a single
// positional argument, which keeps the invocation portable across shells.
func buildPrompt(raw string) string {
	return fmt.Sprintf("%s\n\n--- BEGIN MEMORY CONTENT ---\n%s\n--- END MEMORY CONTENT ---", systemPrompt, raw)
}

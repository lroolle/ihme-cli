package agentkit

// Skill is a named operational procedure. Instructions are the
// procedure text (for embedded files: frontmatter already stripped
// by the consumer at embed time). There is no discovery and no
// registry: the consumer decides which skill to invoke and when.
type Skill struct {
	Name         string
	Instructions string
}

// Invocation wraps the skill and the user's task into a task-turn
// message. The skill is deliberately NOT the system prompt: the
// system prompt holds stable executor rules; the skill is a
// procedure the model follows for this task.
func (s Skill) Invocation(task string) Message {
	return Message{
		Role: RoleUser,
		Text: "Follow this operating procedure to complete the task.\n\n" +
			"<procedure name=\"" + s.Name + "\">\n" + s.Instructions + "\n</procedure>\n\n" +
			"Task: " + task,
	}
}

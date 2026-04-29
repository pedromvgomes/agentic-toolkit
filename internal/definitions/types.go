// Package definitions models the agentic-toolkit definition catalog.
// Every asset in definitions/ deserializes into one of the typed structs
// declared here. The structs are the canonical schema; SCHEMA.md is
// generated from them via tools/schemagen.
package definitions

//go:generate go run ../../tools/schemagen

// Category names a definition kind. The category is encoded both by the
// directory it lives in (definitions/<category>/...) and by the type field
// in the file's frontmatter. Parser cross-checks the two.
type Category string

const (
	CategorySkill       Category = "skill"
	CategoryAgent       Category = "agent"
	CategoryCommand     Category = "command"
	CategoryRule        Category = "rule"
	CategoryInstruction Category = "instruction"
	CategoryHook        Category = "hook"
	CategoryMCP         Category = "mcp"
)

// AllCategories lists the seven category kinds in their canonical order.
var AllCategories = []Category{
	CategorySkill,
	CategoryAgent,
	CategoryCommand,
	CategoryRule,
	CategoryInstruction,
	CategoryHook,
	CategoryMCP,
}

// CategoryDir returns the directory name (under definitions/) for a category.
func (c Category) CategoryDir() string {
	switch c {
	case CategorySkill:
		return "skills"
	case CategoryAgent:
		return "agents"
	case CategoryCommand:
		return "commands"
	case CategoryRule:
		return "rules"
	case CategoryInstruction:
		return "instructions"
	case CategoryHook:
		return "hooks"
	case CategoryMCP:
		return "mcp"
	}
	return ""
}

// CategoryFromDir returns the Category for a directory name. Empty Category
// when the dir is unknown.
func CategoryFromDir(dir string) Category {
	for _, c := range AllCategories {
		if c.CategoryDir() == dir {
			return c
		}
	}
	return ""
}

// Platform names a target agentic-coding platform. Adapters render the
// canonical schema into each platform's native format.
type Platform string

const (
	PlatformClaude   Platform = "claude"
	PlatformCursor   Platform = "cursor"
	PlatformCopilot  Platform = "copilot"
	PlatformOpenCode Platform = "opencode"
	PlatformAgents   Platform = "agents"
)

// AllPlatforms is the canonical list of supported platforms.
var AllPlatforms = []Platform{
	PlatformClaude,
	PlatformCursor,
	PlatformCopilot,
	PlatformOpenCode,
	PlatformAgents,
}

// IsKnownPlatform reports whether p is in AllPlatforms.
func IsKnownPlatform(p Platform) bool {
	for _, k := range AllPlatforms {
		if k == p {
			return true
		}
	}
	return false
}

// Common is embedded by every category struct. It carries the metadata
// needed to identify, describe, and route a definition across platforms.
//
// Category is determined by file location (definitions/<category>/...),
// not by a frontmatter field — there is no `type:` field in YAML. The
// parser sets the in-memory category via Definition.Category().
type Common struct {
	Name        string     `yaml:"name,omitempty"      agtkdoc:"Optional. If present, must equal the path-derived name. If absent, derived from the file path."`
	Description string     `yaml:"description"         agtkdoc:"required;One-line summary used by adapters and discovery surfaces."`
	Platforms   []Platform `yaml:"platforms,omitempty" agtkdoc:"Target platform allowlist. Omit when the definition works on every platform; only set this to *narrow* (e.g. a Claude-only skill that depends on Claude features)."`
	Tags        []string   `yaml:"tags,omitempty"      agtkdoc:"Free-form tags for grouping and filtering."`
	Requires    []string   `yaml:"requires,omitempty"  agtkdoc:"Cross-references to other definitions in 'category/name' form (e.g. skills/challenge)."`
}

// Definition is implemented by every category struct.
type Definition interface {
	GetCommon() *Common
	Category() Category
	// validate runs cross-field checks not expressible in YAML decode.
	// Path is the source file path, used in error messages.
	validate(path string) error
}

// ===== Skill =====

type Skill struct {
	Common     `yaml:",inline"`
	Extensions SkillExtensions `yaml:"extensions,omitempty" agtkdoc:"Per-platform extension blocks."`
	Body       string          `yaml:"-"`
}

type SkillExtensions struct {
	Claude *ClaudeSkillExt `yaml:"claude,omitempty"`
}

type ClaudeSkillExt struct {
	AllowedTools []string `yaml:"allowed_tools,omitempty" agtkdoc:"Tool allowlist for the skill (Claude-specific)."`
}

func (s *Skill) GetCommon() *Common { return &s.Common }
func (*Skill) Category() Category   { return CategorySkill }

// ===== Rule =====

type Rule struct {
	Common     `yaml:",inline"`
	Paths      []string       `yaml:"paths,omitempty"      agtkdoc:"Doublestar globs scoping when this rule auto-attaches. Empty + always=false means manual-only."`
	Always     bool           `yaml:"always,omitempty"     agtkdoc:"If true, the rule always applies regardless of paths."`
	Extensions RuleExtensions `yaml:"extensions,omitempty" agtkdoc:"Per-platform extension blocks."`
	Body       string         `yaml:"-"`
}

type RuleExtensions struct {
	Cursor *CursorRuleExt `yaml:"cursor,omitempty"`
}

type CursorRuleExt struct {
	Description string `yaml:"description,omitempty" agtkdoc:"Cursor-specific description override emitted into .mdc frontmatter."`
}

func (r *Rule) GetCommon() *Common { return &r.Common }
func (*Rule) Category() Category   { return CategoryRule }

// ===== Instruction =====

type Instruction struct {
	Common `yaml:",inline"`
	Body   string `yaml:"-"`
}

func (i *Instruction) GetCommon() *Common { return &i.Common }
func (*Instruction) Category() Category   { return CategoryInstruction }

// ===== Agent =====

type Agent struct {
	Common     `yaml:",inline"`
	Model      string          `yaml:"model,omitempty"      agtkdoc:"Model shorthand: inherit, sonnet, opus, haiku, or a full model id."`
	Tools      []string        `yaml:"tools,omitempty"      agtkdoc:"Tool allowlist (Claude tool-name vocabulary). Empty = inherit."`
	Color      AgentColor      `yaml:"color,omitempty"      agtkdoc:"UI color hint. One of: red, blue, green, yellow, purple, orange, pink, cyan."`
	Extensions AgentExtensions `yaml:"extensions,omitempty" agtkdoc:"Per-platform extension blocks."`
	Body       string          `yaml:"-"`
}

type AgentColor string

const (
	AgentColorRed    AgentColor = "red"
	AgentColorBlue   AgentColor = "blue"
	AgentColorGreen  AgentColor = "green"
	AgentColorYellow AgentColor = "yellow"
	AgentColorPurple AgentColor = "purple"
	AgentColorOrange AgentColor = "orange"
	AgentColorPink   AgentColor = "pink"
	AgentColorCyan   AgentColor = "cyan"
)

// AllAgentColors enumerates accepted color values.
var AllAgentColors = []AgentColor{
	AgentColorRed, AgentColorBlue, AgentColorGreen, AgentColorYellow,
	AgentColorPurple, AgentColorOrange, AgentColorPink, AgentColorCyan,
}

type AgentExtensions struct {
	Claude   *ClaudeAgentExt   `yaml:"claude,omitempty"`
	Cursor   *CursorAgentExt   `yaml:"cursor,omitempty"`
	OpenCode *OpenCodeAgentExt `yaml:"opencode,omitempty"`
}

type ClaudeAgentExt struct {
	DisallowedTools []string `yaml:"disallowed_tools,omitempty" agtkdoc:"Denylist applied before the canonical Tools allowlist."`
	PermissionMode  string   `yaml:"permission_mode,omitempty"  agtkdoc:"Claude permission mode override (default, acceptEdits, plan, ...)."`
	MaxTurns        int      `yaml:"max_turns,omitempty"        agtkdoc:"Hard cap on agentic turns."`
	Memory          string   `yaml:"memory,omitempty"           agtkdoc:"Persistent memory scope: user, project, or local."`
	Background      bool     `yaml:"background,omitempty"       agtkdoc:"Run as a background task."`
	Effort          string   `yaml:"effort,omitempty"           agtkdoc:"Effort level: low, medium, high, xhigh, max."`
	Isolation       string   `yaml:"isolation,omitempty"        agtkdoc:"Set to 'worktree' to isolate the agent in a git worktree."`
	InitialPrompt   string   `yaml:"initial_prompt,omitempty"   agtkdoc:"Auto-submitted first turn when run as the main agent."`
}

type CursorAgentExt struct {
	ReadOnly     bool `yaml:"readonly,omitempty"      agtkdoc:"Restrict the agent to read-only tools."`
	IsBackground bool `yaml:"is_background,omitempty" agtkdoc:"Run as a non-blocking parallel agent."`
}

type OpenCodeAgentExt struct {
	Mode        string  `yaml:"mode,omitempty"        agtkdoc:"OpenCode agent mode: primary, subagent, or all."`
	Temperature float64 `yaml:"temperature,omitempty" agtkdoc:"Sampling temperature."`
	TopP        float64 `yaml:"top_p,omitempty"       agtkdoc:"Top-p nucleus sampling."`
	Hidden      bool    `yaml:"hidden,omitempty"      agtkdoc:"Hide the agent from the picker (subagents only)."`
	Steps       int     `yaml:"steps,omitempty"       agtkdoc:"Maximum iterations."`
}

func (a *Agent) GetCommon() *Common { return &a.Common }
func (*Agent) Category() Category   { return CategoryAgent }

// ===== Command =====

type Command struct {
	Common       `yaml:",inline"`
	Model        string            `yaml:"model,omitempty"         agtkdoc:"Model shorthand or full model id."`
	Tools        []string          `yaml:"tools,omitempty"         agtkdoc:"Tool allowlist. Empty = inherit."`
	ArgumentHint string            `yaml:"argument_hint,omitempty" agtkdoc:"Free-form hint shown in the command picker (e.g. \"<branch-name>\")."`
	Extensions   CommandExtensions `yaml:"extensions,omitempty"    agtkdoc:"Per-platform extension blocks."`
	Body         string            `yaml:"-"`
}

type CommandExtensions struct {
	OpenCode *OpenCodeCmdExt `yaml:"opencode,omitempty"`
	Copilot  *CopilotCmdExt  `yaml:"copilot,omitempty"`
}

type OpenCodeCmdExt struct {
	Agent   string `yaml:"agent,omitempty"   agtkdoc:"OpenCode agent that runs this command."`
	Subtask bool   `yaml:"subtask,omitempty" agtkdoc:"If true, force execution as a subagent."`
}

type CopilotCmdExt struct {
	Mode string `yaml:"mode,omitempty" agtkdoc:"Copilot chat mode: ask, edit, or agent."`
}

func (c *Command) GetCommon() *Common { return &c.Common }
func (*Command) Category() Category   { return CategoryCommand }

// ===== Hook =====

type Hook struct {
	Common     `yaml:",inline"`
	Event      string         `yaml:"event"                agtkdoc:"required;Lifecycle event the hook attaches to. See SCHEMA.md for portable vs platform-specific events."`
	Matcher    string         `yaml:"matcher,omitempty"    agtkdoc:"Regex (or pipe-separated list) matching the event payload. Empty = match all."`
	Handler    HookHandler    `yaml:"handler"              agtkdoc:"required;What to run when the hook fires."`
	FailClosed bool           `yaml:"fail_closed,omitempty" agtkdoc:"If true, a non-zero exit blocks the action; default is fail-open."`
	Timeout    int            `yaml:"timeout,omitempty"    agtkdoc:"Timeout in milliseconds; 0 = platform default."`
	Extensions HookExtensions `yaml:"extensions,omitempty" agtkdoc:"Per-platform extension blocks."`
}

type HookHandler struct {
	Type    HandlerType `yaml:"type"              agtkdoc:"required;Handler kind: command or prompt."`
	Command string      `yaml:"command,omitempty" agtkdoc:"Shell command (handler type=command)."`
	Prompt  string      `yaml:"prompt,omitempty"  agtkdoc:"Prompt template (handler type=prompt)."`
	Model   string      `yaml:"model,omitempty"   agtkdoc:"Model override for prompt-type handlers."`
}

type HandlerType string

const (
	HandlerCommand HandlerType = "command"
	HandlerPrompt  HandlerType = "prompt"
)

// AllHandlerTypes enumerates accepted canonical handler type values.
var AllHandlerTypes = []HandlerType{HandlerCommand, HandlerPrompt}

type HookExtensions struct {
	Claude *ClaudeHookExt `yaml:"claude,omitempty"`
	Cursor *CursorHookExt `yaml:"cursor,omitempty"`
}

type ClaudeHookExt struct {
	HTTPHandler    *ClaudeHTTPHandler    `yaml:"http,omitempty"           agtkdoc:"Claude-only http handler config."`
	MCPToolHandler *ClaudeMCPToolHandler `yaml:"mcp_tool,omitempty"  agtkdoc:"Claude-only mcp_tool handler config."`
	AgentHandler   *ClaudeAgentHandler   `yaml:"agent,omitempty"        agtkdoc:"Claude-only agent handler config."`
	StatusMessage  string                `yaml:"status_message,omitempty" agtkdoc:"UI message shown while the hook runs."`
	Once           bool                  `yaml:"once,omitempty"           agtkdoc:"Fire only once per session."`
}

type ClaudeHTTPHandler struct {
	URL            string            `yaml:"url"`
	Headers        map[string]string `yaml:"headers,omitempty"`
	AllowedEnvVars []string          `yaml:"allowed_env_vars,omitempty"`
}

type ClaudeMCPToolHandler struct {
	Server string                 `yaml:"server"`
	Tool   string                 `yaml:"tool"`
	Input  map[string]interface{} `yaml:"input,omitempty"`
}

type ClaudeAgentHandler struct {
	Prompt string `yaml:"prompt"`
	Model  string `yaml:"model,omitempty"`
}

type CursorHookExt struct {
	LoopLimit int `yaml:"loop_limit,omitempty" agtkdoc:"Cursor-specific loop guard for hooks that re-trigger themselves."`
}

func (h *Hook) GetCommon() *Common { return &h.Common }
func (*Hook) Category() Category   { return CategoryHook }

// ===== MCPServer =====

type MCPServer struct {
	Common     `yaml:",inline"`
	Transport  Transport         `yaml:"transport"            agtkdoc:"required;One of stdio, http, sse."`
	Command    string            `yaml:"command,omitempty"    agtkdoc:"Executable for stdio transport. Supports ${VAR} expansion."`
	Args       []string          `yaml:"args,omitempty"       agtkdoc:"Arguments for stdio transport."`
	Env        map[string]string `yaml:"env,omitempty"        agtkdoc:"Environment variables for stdio transport."`
	URL        string            `yaml:"url,omitempty"        agtkdoc:"Endpoint URL for http/sse transport."`
	Headers    map[string]string `yaml:"headers,omitempty"    agtkdoc:"Static request headers for http/sse transport."`
	OAuth      *OAuthConfig      `yaml:"oauth,omitempty"      agtkdoc:"OAuth configuration for http/sse transport."`
	Extensions MCPExtensions     `yaml:"extensions,omitempty" agtkdoc:"Per-platform extension blocks."`
}

type Transport string

const (
	TransportStdio Transport = "stdio"
	TransportHTTP  Transport = "http"
	TransportSSE   Transport = "sse"
)

// AllTransports enumerates the canonical transport values.
var AllTransports = []Transport{TransportStdio, TransportHTTP, TransportSSE}

type OAuthConfig struct {
	ClientID              string   `yaml:"client_id,omitempty"                agtkdoc:"Pre-configured OAuth client id."`
	CallbackPort          int      `yaml:"callback_port,omitempty"            agtkdoc:"Fixed local port for the OAuth callback."`
	AuthServerMetadataURL string   `yaml:"auth_server_metadata_url,omitempty" agtkdoc:"OAuth metadata discovery URL override."`
	Scopes                []string `yaml:"scopes,omitempty"                   agtkdoc:"Requested OAuth scopes (canonical: array; Claude adapter joins to a space-separated string)."`
}

type MCPExtensions struct {
	Claude   *ClaudeMCPExt   `yaml:"claude,omitempty"`
	OpenCode *OpenCodeMCPExt `yaml:"opencode,omitempty"`
}

type ClaudeMCPExt struct {
	HeadersHelper string `yaml:"headers_helper,omitempty" agtkdoc:"Path or command emitting JSON headers at connection time (Claude-only)."`
	WSURL         string `yaml:"ws_url,omitempty"         agtkdoc:"WebSocket URL for Claude's ws transport (out of canonical)."`
}

type OpenCodeMCPExt struct {
	Enabled *bool `yaml:"enabled,omitempty" agtkdoc:"OpenCode-specific enable toggle."`
	Timeout int   `yaml:"timeout,omitempty" agtkdoc:"OpenCode-specific connect timeout in ms."`
}

func (m *MCPServer) GetCommon() *Common { return &m.Common }
func (*MCPServer) Category() Category   { return CategoryMCP }

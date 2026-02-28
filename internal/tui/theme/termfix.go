package theme

import (
	"github.com/charmbracelet/lipgloss"
)

type TermfixTheme struct {
	BaseTheme
}

func NewTermfixTheme() *TermfixTheme {
	// Dark mode colors — amber/red troubleshooting palette
	darkBackground := "#1a1a1a"
	darkCurrentLine := "#222222"
	darkSelection := "#333333"
	darkForeground := "#d4d4d4"
	darkComment := "#6a6a6a"
	darkPrimary := "#f0a020"   // Amber — alerts/warnings
	darkSecondary := "#e08030" // Orange
	darkAccent := "#ff6b6b"    // Red — critical/errors
	darkRed := "#ff6b6b"
	darkOrange := "#f0a020"
	darkGreen := "#44ff44"
	darkCyan := "#56b6c2"
	darkYellow := "#e5c07b"
	darkBorder := "#444444"

	// Light mode colors
	lightBackground := "#f5f5f0"
	lightCurrentLine := "#eaeae5"
	lightSelection := "#ddddd8"
	lightForeground := "#2a2a2a"
	lightComment := "#8a8a8a"
	lightPrimary := "#c07800"
	lightSecondary := "#a06020"
	lightAccent := "#cc3333"
	lightRed := "#cc3333"
	lightOrange := "#c07800"
	lightGreen := "#2d8a2d"
	lightCyan := "#2a7a7a"
	lightYellow := "#8a6a00"
	lightBorder := "#c0c0b0"

	theme := &TermfixTheme{}

	theme.PrimaryColor = lipgloss.AdaptiveColor{Dark: darkPrimary, Light: lightPrimary}
	theme.SecondaryColor = lipgloss.AdaptiveColor{Dark: darkSecondary, Light: lightSecondary}
	theme.AccentColor = lipgloss.AdaptiveColor{Dark: darkAccent, Light: lightAccent}

	theme.ErrorColor = lipgloss.AdaptiveColor{Dark: darkRed, Light: lightRed}
	theme.WarningColor = lipgloss.AdaptiveColor{Dark: darkOrange, Light: lightOrange}
	theme.SuccessColor = lipgloss.AdaptiveColor{Dark: darkGreen, Light: lightGreen}
	theme.InfoColor = lipgloss.AdaptiveColor{Dark: darkCyan, Light: lightCyan}

	theme.TextColor = lipgloss.AdaptiveColor{Dark: darkForeground, Light: lightForeground}
	theme.TextMutedColor = lipgloss.AdaptiveColor{Dark: darkComment, Light: lightComment}
	theme.TextEmphasizedColor = lipgloss.AdaptiveColor{Dark: darkYellow, Light: lightYellow}

	theme.BackgroundColor = lipgloss.AdaptiveColor{Dark: darkBackground, Light: lightBackground}
	theme.BackgroundSecondaryColor = lipgloss.AdaptiveColor{Dark: darkCurrentLine, Light: lightCurrentLine}
	theme.BackgroundDarkerColor = lipgloss.AdaptiveColor{Dark: "#111111", Light: "#ffffff"}

	theme.BorderNormalColor = lipgloss.AdaptiveColor{Dark: darkBorder, Light: lightBorder}
	theme.BorderFocusedColor = lipgloss.AdaptiveColor{Dark: darkPrimary, Light: lightPrimary}
	theme.BorderDimColor = lipgloss.AdaptiveColor{Dark: darkSelection, Light: lightSelection}

	theme.DiffAddedColor = lipgloss.AdaptiveColor{Dark: "#478247", Light: "#2E7D32"}
	theme.DiffRemovedColor = lipgloss.AdaptiveColor{Dark: "#7C4444", Light: "#C62828"}
	theme.DiffContextColor = lipgloss.AdaptiveColor{Dark: "#a0a0a0", Light: "#757575"}
	theme.DiffHunkHeaderColor = lipgloss.AdaptiveColor{Dark: "#a0a0a0", Light: "#757575"}
	theme.DiffHighlightAddedColor = lipgloss.AdaptiveColor{Dark: "#DAFADA", Light: "#A5D6A7"}
	theme.DiffHighlightRemovedColor = lipgloss.AdaptiveColor{Dark: "#FADADD", Light: "#EF9A9A"}
	theme.DiffAddedBgColor = lipgloss.AdaptiveColor{Dark: "#303A30", Light: "#E8F5E9"}
	theme.DiffRemovedBgColor = lipgloss.AdaptiveColor{Dark: "#3A3030", Light: "#FFEBEE"}
	theme.DiffContextBgColor = lipgloss.AdaptiveColor{Dark: darkBackground, Light: lightBackground}
	theme.DiffLineNumberColor = lipgloss.AdaptiveColor{Dark: "#888888", Light: "#9E9E9E"}
	theme.DiffAddedLineNumberBgColor = lipgloss.AdaptiveColor{Dark: "#293229", Light: "#C8E6C9"}
	theme.DiffRemovedLineNumberBgColor = lipgloss.AdaptiveColor{Dark: "#332929", Light: "#FFCDD2"}

	theme.MarkdownTextColor = lipgloss.AdaptiveColor{Dark: darkForeground, Light: lightForeground}
	theme.MarkdownHeadingColor = lipgloss.AdaptiveColor{Dark: darkPrimary, Light: lightPrimary}
	theme.MarkdownLinkColor = lipgloss.AdaptiveColor{Dark: darkSecondary, Light: lightSecondary}
	theme.MarkdownLinkTextColor = lipgloss.AdaptiveColor{Dark: darkCyan, Light: lightCyan}
	theme.MarkdownCodeColor = lipgloss.AdaptiveColor{Dark: darkGreen, Light: lightGreen}
	theme.MarkdownBlockQuoteColor = lipgloss.AdaptiveColor{Dark: darkYellow, Light: lightYellow}
	theme.MarkdownEmphColor = lipgloss.AdaptiveColor{Dark: darkYellow, Light: lightYellow}
	theme.MarkdownStrongColor = lipgloss.AdaptiveColor{Dark: darkAccent, Light: lightAccent}
	theme.MarkdownHorizontalRuleColor = lipgloss.AdaptiveColor{Dark: darkComment, Light: lightComment}
	theme.MarkdownListItemColor = lipgloss.AdaptiveColor{Dark: darkPrimary, Light: lightPrimary}
	theme.MarkdownListEnumerationColor = lipgloss.AdaptiveColor{Dark: darkCyan, Light: lightCyan}
	theme.MarkdownImageColor = lipgloss.AdaptiveColor{Dark: darkPrimary, Light: lightPrimary}
	theme.MarkdownImageTextColor = lipgloss.AdaptiveColor{Dark: darkCyan, Light: lightCyan}
	theme.MarkdownCodeBlockColor = lipgloss.AdaptiveColor{Dark: darkForeground, Light: lightForeground}

	theme.SyntaxCommentColor = lipgloss.AdaptiveColor{Dark: darkComment, Light: lightComment}
	theme.SyntaxKeywordColor = lipgloss.AdaptiveColor{Dark: darkSecondary, Light: lightSecondary}
	theme.SyntaxFunctionColor = lipgloss.AdaptiveColor{Dark: darkPrimary, Light: lightPrimary}
	theme.SyntaxVariableColor = lipgloss.AdaptiveColor{Dark: darkRed, Light: lightRed}
	theme.SyntaxStringColor = lipgloss.AdaptiveColor{Dark: darkGreen, Light: lightGreen}
	theme.SyntaxNumberColor = lipgloss.AdaptiveColor{Dark: darkAccent, Light: lightAccent}
	theme.SyntaxTypeColor = lipgloss.AdaptiveColor{Dark: darkYellow, Light: lightYellow}
	theme.SyntaxOperatorColor = lipgloss.AdaptiveColor{Dark: darkCyan, Light: lightCyan}
	theme.SyntaxPunctuationColor = lipgloss.AdaptiveColor{Dark: darkForeground, Light: lightForeground}

	return theme
}

func init() {
	RegisterTheme("termfix", NewTermfixTheme())
}

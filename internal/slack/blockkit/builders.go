package blockkit

import (
	"encoding/json"
	"fmt"

	"github.com/slack-go/slack"
)

// RawBlock is a passthrough for block types not yet supported by slack-go
// (e.g. the new "markdown" block). It implements slack.Block and marshals
// back to the original JSON.
type RawBlock struct {
	RawType string
	Raw     json.RawMessage
}

func (b *RawBlock) BlockType() slack.MessageBlockType {
	return slack.MessageBlockType(b.RawType)
}

func (b *RawBlock) ID() string { return "" }

func (b *RawBlock) MarshalJSON() ([]byte, error) {
	return b.Raw, nil
}

// Header returns a header block with the given plain text.
func Header(text string) *slack.HeaderBlock {
	return slack.NewHeaderBlock(
		slack.NewTextBlockObject(slack.PlainTextType, text, true, false),
	)
}

// Section returns a section block with mrkdwn text.
func Section(mrkdwn string) *slack.SectionBlock {
	return slack.NewSectionBlock(
		slack.NewTextBlockObject(slack.MarkdownType, mrkdwn, false, false),
		nil, nil,
	)
}

// SectionWithFields returns a section block with paired field columns.
// Each pair is rendered as a bold label + value in a two-column layout.
func SectionWithFields(pairs [][2]string) *slack.SectionBlock {
	fields := make([]*slack.TextBlockObject, 0, len(pairs)*2)
	for _, p := range pairs {
		fields = append(fields,
			slack.NewTextBlockObject(slack.MarkdownType, "*"+p[0]+"*", false, false),
			slack.NewTextBlockObject(slack.MarkdownType, p[1], false, false),
		)
	}
	return slack.NewSectionBlock(nil, fields, nil)
}

// Divider returns a divider block.
func Divider() *slack.DividerBlock {
	return slack.NewDividerBlock()
}

// Context returns a context block containing one or more mrkdwn text elements.
func Context(blockID string, texts ...string) *slack.ContextBlock {
	elements := make([]slack.MixedElement, 0, len(texts))
	for _, t := range texts {
		elements = append(elements, slack.NewTextBlockObject(slack.MarkdownType, t, false, false))
	}
	return slack.NewContextBlock(blockID, elements...)
}

// Actions returns an action block containing the given buttons.
func Actions(blockID string, buttons ...*slack.ButtonBlockElement) *slack.ActionBlock {
	elements := make([]slack.BlockElement, 0, len(buttons))
	for _, b := range buttons {
		elements = append(elements, b)
	}
	return slack.NewActionBlock(blockID, elements...)
}

// Button returns a plain button element.
func Button(actionID, label, value string) *slack.ButtonBlockElement {
	return slack.NewButtonBlockElement(actionID, value,
		slack.NewTextBlockObject(slack.PlainTextType, label, true, false),
	)
}

// PrimaryButton returns a button styled as primary (green).
func PrimaryButton(actionID, label, value string) *slack.ButtonBlockElement {
	btn := Button(actionID, label, value)
	btn.Style = slack.StylePrimary
	return btn
}

// DangerButton returns a button styled as danger (red).
func DangerButton(actionID, label, value string) *slack.ButtonBlockElement {
	btn := Button(actionID, label, value)
	btn.Style = slack.StyleDanger
	return btn
}

// ParseBlocksJSON parses raw Block Kit JSON (an array of block objects) into
// a slice of slack.Block values. It returns the blocks, a diagnostic string,
// and any error.
//
// The input must be a JSON array where each element has a "type" field.
func ParseBlocksJSON(data []byte) ([]slack.Block, string, error) {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, "", fmt.Errorf("blockkit: invalid JSON array: %w", err)
	}

	blocks := make([]slack.Block, 0, len(raw))
	for i, r := range raw {
		var peek struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(r, &peek); err != nil {
			return nil, "", fmt.Errorf("blockkit: block %d: %w", i, err)
		}

		var block slack.Block
		switch peek.Type {
		case "header":
			var b slack.HeaderBlock
			if err := json.Unmarshal(r, &b); err != nil {
				return nil, "", fmt.Errorf("blockkit: header block %d: %w", i, err)
			}
			block = &b
		case "section":
			var b slack.SectionBlock
			if err := json.Unmarshal(r, &b); err != nil {
				return nil, "", fmt.Errorf("blockkit: section block %d: %w", i, err)
			}
			block = &b
		case "divider":
			var b slack.DividerBlock
			if err := json.Unmarshal(r, &b); err != nil {
				return nil, "", fmt.Errorf("blockkit: divider block %d: %w", i, err)
			}
			block = &b
		case "context":
			var b slack.ContextBlock
			if err := json.Unmarshal(r, &b); err != nil {
				return nil, "", fmt.Errorf("blockkit: context block %d: %w", i, err)
			}
			block = &b
		case "actions":
			var b slack.ActionBlock
			if err := json.Unmarshal(r, &b); err != nil {
				return nil, "", fmt.Errorf("blockkit: actions block %d: %w", i, err)
			}
			block = &b
		case "image":
			var b slack.ImageBlock
			if err := json.Unmarshal(r, &b); err != nil {
				return nil, "", fmt.Errorf("blockkit: image block %d: %w", i, err)
			}
			block = &b
		case "input":
			var b slack.InputBlock
			if err := json.Unmarshal(r, &b); err != nil {
				return nil, "", fmt.Errorf("blockkit: input block %d: %w", i, err)
			}
			block = &b
		default:
			// For unknown/new block types (e.g. "markdown"), pass through as raw JSON.
			block = &RawBlock{RawType: peek.Type, Raw: r}
		}
		blocks = append(blocks, block)
	}

	diag := fmt.Sprintf("parsed %d block(s)", len(blocks))
	return blocks, diag, nil
}

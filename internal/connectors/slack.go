package connectors

import (
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/nodes"
)

// Slack declares the Slack Web API.
//
// Slack answers 200 for a rejected call and puts the verdict in the body, so
// OKField/ErrorField are what make a failure look like one. Without them
// "channel not found" would flow downstream as a perfectly successful item.
func Slack() Connector {
	return Connector{
		ID:          "slack",
		Name:        "Slack",
		Icon:        "slack",
		Description: "Posts and reads messages in a Slack workspace.",
		Credential:  "slackOAuth2",
		BaseURL:     "https://slack.com/api",
		OKField:     "ok",
		ErrorField:  "error",
		Operations: []Operation{
			{
				ID:          "postMessage",
				Name:        "Send a message",
				Description: "Posts a message to a channel with chat.postMessage.",
				Method:      "POST",
				Path:        "/chat.postMessage",
				Params: []nodes.ParamSpec{
					slackChannel(),
					{
						Name:               "text",
						Label:              "Text",
						Type:               nodes.ParamString,
						Required:           true,
						Placeholder:        "Deploy finished: {{ $json.version }}",
						Description:        "The message body, in Slack's mrkdwn format.",
						SupportsExpression: true,
					},
					{
						Name:               "thread_ts",
						Label:              "Thread Timestamp",
						Type:               nodes.ParamString,
						Placeholder:        "1710000000.123456",
						Description:        "Leave empty to post to the channel. Set it to a message's ts to reply in that message's thread.",
						SupportsExpression: true,
					},
				},
				// The placeholders sit where JSON values go, unquoted: the
				// executor substitutes the encoded value, so a message full of
				// quotes and newlines cannot break the document.
				Body: `{"channel":{channel},"text":{text},"thread_ts":{thread_ts}}`,
			},
			{
				ID:          "listChannels",
				Name:        "List channels",
				Description: "Lists the conversations the workspace has, one item per channel, via conversations.list.",
				Method:      "GET",
				Path:        "/conversations.list",
				Params: []nodes.ParamSpec{
					{
						Name:               "types",
						Label:              "Types",
						Type:               nodes.ParamString,
						Default:            "public_channel",
						Description:        "Comma-separated: public_channel, private_channel, mpim, im.",
						SupportsExpression: true,
					},
					slackLimit(),
				},
				Query: map[string]string{
					"types": "{types}",
					"limit": "{limit}",
				},
				ItemsPath: "channels",
			},
			{
				ID:          "channelHistory",
				Name:        "Get channel history",
				Description: "Reads a channel's recent messages, one item per message, via conversations.history.",
				Method:      "GET",
				Path:        "/conversations.history",
				Params: []nodes.ParamSpec{
					slackChannel(),
					slackLimit(),
				},
				Query: map[string]string{
					"channel": "{channel}",
					"limit":   "{limit}",
				},
				ItemsPath: "messages",
			},
		},
	}
}

// slackChannel is shared by posting and reading. Two operations may share a
// parameter only when the whole spec matches, so the shared ones are written
// once here rather than copied — and the wording has to suit both, which is why
// it describes the channel instead of what is about to be done to it.
func slackChannel() nodes.ParamSpec {
	return nodes.ParamSpec{
		Name:               "channel",
		Label:              "Channel",
		Type:               nodes.ParamString,
		Required:           true,
		Placeholder:        "C0123456789",
		Description:        "Channel ID, or #name for a public channel. The app must be a member of it.",
		SupportsExpression: true,
	}
}

// slackLimit is shared by the two paginated reads.
func slackLimit() nodes.ParamSpec {
	return nodes.ParamSpec{
		Name:               "limit",
		Label:              "Limit",
		Type:               nodes.ParamNumber,
		Default:            100,
		Description:        "How many results to ask for in one page. Slack recommends no more than 200.",
		SupportsExpression: true,
	}
}

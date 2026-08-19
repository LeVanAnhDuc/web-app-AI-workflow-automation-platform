package connectors

import (
	"github.com/LeVanAnhDuc/app-AI-workflow-automation-platform/internal/nodes"
)

// GoogleSheets declares the Sheets v4 values API.
//
// Both operations put the A1 range in the path, and a range legitimately
// contains spaces and can contain a slash ("'Q1/2026'!A:D"). That is exactly
// what the executor's path escaping is for: substituted values are escaped, so
// a range can never invent a path segment.
func GoogleSheets() Connector {
	return Connector{
		ID:          "googleSheets",
		Name:        "Google Sheets",
		Icon:        "table",
		Description: "Reads and appends rows in a Google spreadsheet.",
		Credential:  "googleSheetsOAuth2",
		BaseURL:     "https://sheets.googleapis.com/v4",
		Operations: []Operation{
			{
				ID:   "readRange",
				Name: "Read a range",
				Description: "Reads a range and returns one item per row. " +
					"A row is an array of cells, so it arrives under the item's \"value\" key.",
				Method:    "GET",
				Path:      "/spreadsheets/{spreadsheetId}/values/{range}",
				Params:    []nodes.ParamSpec{sheetID(), sheetRange()},
				ItemsPath: "values",
			},
			{
				ID:          "appendRow",
				Name:        "Append rows",
				Description: "Appends rows after the last row of the range, and returns the API's update summary as one item.",
				Method:      "POST",
				Path:        "/spreadsheets/{spreadsheetId}/values/{range}:append",
				Params: []nodes.ParamSpec{
					sheetID(),
					sheetRange(),
					{
						Name:               "values",
						Label:              "Rows",
						Type:               nodes.ParamJSON,
						Required:           true,
						Default:            `[["a", "b"]]`,
						Placeholder:        `[["{{ $json.name }}", "{{ $json.email }}"]]`,
						Description:        "An array of rows, each row an array of cell values.",
						SupportsExpression: true,
					},
					{
						Name:    "valueInputOption",
						Label:   "Input Mode",
						Type:    nodes.ParamSelect,
						Default: "USER_ENTERED",
						Options: []nodes.ParamOption{
							{Label: "User entered — parse dates and formulas", Value: "USER_ENTERED"},
							{Label: "Raw — store the text exactly", Value: "RAW"},
						},
						Description: "How Sheets should interpret the values, as if typed or verbatim.",
					},
				},
				Query: map[string]string{"valueInputOption": "{valueInputOption}"},
				Body:  `{"values":{values}}`,
			},
		},
	}
}

// sheetID and sheetRange are shared by both operations, so they are written once:
// a parameter name may repeat across operations only when the whole spec matches.
func sheetID() nodes.ParamSpec {
	return nodes.ParamSpec{
		Name:               "spreadsheetId",
		Label:              "Spreadsheet ID",
		Type:               nodes.ParamString,
		Required:           true,
		Placeholder:        "1BxiMVs0XRA5nFMdKvBdBZjgmUUqptlbs74OgvE2upms",
		Description:        "The long id in the spreadsheet's URL, between /d/ and /edit.",
		SupportsExpression: true,
	}
}

func sheetRange() nodes.ParamSpec {
	return nodes.ParamSpec{
		Name:               "range",
		Label:              "Range",
		Type:               nodes.ParamString,
		Required:           true,
		Default:            "Sheet1!A:D",
		Placeholder:        "Sheet1!A1:D100",
		Description:        "A1 notation. A sheet name containing a space or a slash must be quoted, as in 'Q1 2026'!A:D.",
		SupportsExpression: true,
	}
}

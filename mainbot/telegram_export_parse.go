package mainbot

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"

	"watgbridge/database"
)

const maxExportJSONBytes = 20 << 20 // match Bot API practical document limit

// ExtractResultJSONFromZIP returns the first result.json from a Telegram Desktop export .zip.
func ExtractResultJSONFromZIP(zipData []byte) ([]byte, error) {
	if len(zipData) > maxExportJSONBytes {
		return nil, fmt.Errorf("zip larger than %d bytes", maxExportJSONBytes)
	}
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, err
	}
	var best []byte
	bestDepth := -1
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := filepath.ToSlash(f.Name)
		if strings.Contains(name, "__MACOSX/") {
			continue
		}
		base := filepath.Base(name)
		if base != "result.json" {
			continue
		}
		depth := strings.Count(name, "/")
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			return nil, err
		}
		if len(body) > maxExportJSONBytes {
			return nil, fmt.Errorf("result.json larger than %d bytes", maxExportJSONBytes)
		}
		if best == nil || depth < bestDepth {
			best = body
			bestDepth = depth
		}
	}
	if len(best) == 0 {
		return nil, fmt.Errorf("no result.json found in zip (export the chat or full data from Telegram Desktop)")
	}
	return best, nil
}

type parsedExportChat struct {
	ID       int64
	Name     string
	Messages []map[string]interface{}
}

// ParseTelegramExportForChat finds the chat whose id matches targetChatID and builds import rows.
func ParseTelegramExportForChat(jsonData []byte, targetChatID int64) (exportChatID int64, rows []database.TelegramImportMessage, err error) {
	if len(jsonData) > maxExportJSONBytes {
		return 0, nil, fmt.Errorf("json larger than %d bytes", maxExportJSONBytes)
	}
	var root map[string]interface{}
	dec := json.NewDecoder(bytes.NewReader(jsonData))
	dec.UseNumber()
	if err := dec.Decode(&root); err != nil {
		return 0, nil, fmt.Errorf("invalid json: %w", err)
	}

	var chat parsedExportChat
	if chats, ok := root["chats"].(map[string]interface{}); ok {
		list, _ := chats["list"].([]interface{})
		if len(list) == 0 {
			return 0, nil, fmt.Errorf("export has no chats (use Telegram Desktop → Settings → Export data, include this group)")
		}
		for _, item := range list {
			cm, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			id := parseChatID(cm["id"])
			if id == targetChatID {
				chat = parsedExportFromMap(cm)
				break
			}
		}
		if chat.ID == 0 {
			return 0, nil, fmt.Errorf("no chat with id %d in export (export includes %d chats)", targetChatID, len(list))
		}
	} else {
		chat = parsedExportFromMap(root)
	}

	if chat.ID == 0 && len(chat.Messages) == 0 {
		return 0, nil, fmt.Errorf("unrecognized export format (expected Telegram Desktop result.json)")
	}
	if chat.ID != targetChatID {
		return 0, nil, fmt.Errorf("export chat id %d does not match bridge target %d", chat.ID, targetChatID)
	}

	rows = make([]database.TelegramImportMessage, 0, len(chat.Messages))
	for _, m := range chat.Messages {
		row := messageToImportRow(m)
		if row.TgMessageID == 0 {
			continue
		}
		rows = append(rows, row)
	}
	return chat.ID, rows, nil
}

func parsedExportFromMap(cm map[string]interface{}) parsedExportChat {
	var c parsedExportChat
	c.ID = parseChatID(cm["id"])
	if name, ok := cm["name"].(string); ok {
		c.Name = name
	}
	raw, _ := json.Marshal(cm["messages"])
	var list []map[string]interface{}
	_ = json.Unmarshal(raw, &list)
	c.Messages = list
	return c
}

func parseChatID(v interface{}) int64 {
	switch x := v.(type) {
	case nil:
		return 0
	case json.Number:
		n, _ := x.Int64()
		return n
	case float64:
		return int64(x)
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		if err != nil {
			return 0
		}
		return n
	}
	return 0
}

func messageToImportRow(m map[string]interface{}) database.TelegramImportMessage {
	id := parseChatID(m["id"])
	msgType, _ := m["type"].(string)
	if msgType == "" {
		msgType = "message"
	}
	action, _ := m["action"].(string)
	from, _ := m["from"].(string)
	fromID := normalizeFromID(m["from_id"])
	dateUnix := parseUnixTimeFlexible(m["date_unixtime"])
	text := flattenExportText(m["text"])
	if text == "" {
		if ents, ok := m["text_entities"].([]interface{}); ok && len(ents) > 0 {
			text = flattenEntities(ents)
		}
	}
	if text == "" && msgType == "service" {
		if action != "" {
			text = "[" + action + "]"
		} else {
			text = "[service]"
		}
	}
	var threadID int64
	if v, ok := m["message_thread_id"]; ok {
		threadID = parseChatID(v)
	}
	return database.TelegramImportMessage{
		TgMessageID:   id,
		TgThreadID:    threadID,
		FromName:      truncateStr(from, 512),
		FromID:        truncateStr(fromID, 256),
		Text:          text,
		MsgType:       truncateStr(msgType, 32),
		ServiceAction: truncateStr(action, 64),
		DateUnix:      dateUnix,
	}
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

func normalizeFromID(v interface{}) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatInt(int64(x), 10)
	case map[string]interface{}:
		if u, ok := x["user_id"]; ok {
			return "user" + stringifyID(u)
		}
		if u, ok := x["channel_id"]; ok {
			return "channel" + stringifyID(u)
		}
		if u, ok := x["chat_id"]; ok {
			return "chat" + stringifyID(u)
		}
	}
	return ""
}

func stringifyID(v interface{}) string {
	switch x := v.(type) {
	case json.Number:
		s, _ := x.Int64()
		return strconv.FormatInt(s, 10)
	case float64:
		return strconv.FormatInt(int64(x), 10)
	case string:
		return x
	}
	return ""
}

func parseUnixTimeFlexible(v interface{}) int64 {
	if v == nil {
		return 0
	}
	switch x := v.(type) {
	case json.Number:
		n, _ := x.Int64()
		return n
	case float64:
		return int64(x)
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		if err != nil {
			return 0
		}
		return n
	}
	return 0
}

func flattenExportText(v interface{}) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case []interface{}:
		var b strings.Builder
		for _, p := range x {
			switch t := p.(type) {
			case string:
				b.WriteString(t)
			case map[string]interface{}:
				if txt, ok := t["text"].(string); ok {
					b.WriteString(txt)
				}
			}
		}
		return b.String()
	}
	return ""
}

func flattenEntities(ents []interface{}) string {
	var b strings.Builder
	for _, e := range ents {
		em, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		typ, _ := em["type"].(string)
		txt, _ := em["text"].(string)
		if typ == "plain" || typ == "unknown" || txt != "" {
			b.WriteString(txt)
		}
	}
	return b.String()
}

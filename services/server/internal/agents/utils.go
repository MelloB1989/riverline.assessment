package agents

import (
	m "riverline_server/internal/models"
	"time"

	"github.com/MelloB1989/karma/models"
	"github.com/MelloB1989/karma/utils"
	"github.com/MelloB1989/karma/v2/orm"
)

func generateAIChatHistory(conversationId string) (*models.AIChatHistory, error) {
	messagesOrm := orm.Load(&m.AgentMessage{})
	defer messagesOrm.Close()

	var messages []m.AgentMessage
	if err := messagesOrm.GetByFieldEquals("ConversationId", conversationId).Scan(&messages); err != nil {
		return nil, err
	}

	history := &models.AIChatHistory{
		Messages: make([]models.AIMessage, 0, len(messages)),
	}
	for _, msg := range messages {
		if msg.Role == m.MessageRoleAgent {
			history.Messages = append(history.Messages, models.AIMessage{
				UniqueId:   msg.Id,
				Role:       models.Assistant,
				Message:    msg.Content,
				Timestamp:  msg.CreatedAt,
				ToolCalls:  msg.ToolCalls,
				ToolCallId: msg.ToolCallId,
			})
		} else {
			history.Messages = append(history.Messages, models.AIMessage{
				UniqueId:  msg.Id,
				Role:      models.User,
				Message:   msg.Content,
				Timestamp: msg.CreatedAt,
				Images:    msg.Images,
				Files:     msg.Files,
			})
		}
	}
	return history, nil
}

func watchAndAppendMessages(history *models.AIChatHistory) {
	go func(history *models.AIChatHistory) {
		lastMessageLen := len(history.Messages)
		for {
			if len(history.Messages) != lastMessageLen {
				lastMessageLen = len(history.Messages)
				lastMessage := history.Messages[lastMessageLen-1]
				if lastMessage.UniqueId == "" {
					lastMessage.UniqueId = utils.GenerateID()
				}
				if err := pushMessageToDB(toAgentMessage(&lastMessage)); err != nil {
					return
				}
			}
			time.Sleep(time.Second)
		}
	}(history)
}

func pushMessageToDB(msg *m.AgentMessage) error {
	messagesOrm := orm.Load(&m.AgentMessage{})
	defer messagesOrm.Close()

	if err := messagesOrm.Insert(msg); err != nil {
		return err
	}
	return nil
}

func updateMessageInDB(msg *m.AgentMessage) error {
	messagesOrm := orm.Load(&m.AgentMessage{})
	defer messagesOrm.Close()

	if err := messagesOrm.Update(msg, msg.Id); err != nil {
		return err
	}
	return nil
}

func toAgentMessage(msg *models.AIMessage) *m.AgentMessage {
	return &m.AgentMessage{
		Id:         msg.UniqueId,
		Content:    msg.Message,
		CreatedAt:  msg.Timestamp,
		Images:     msg.Images,
		Files:      msg.Files,
		ToolCalls:  msg.ToolCalls,
		ToolCallId: msg.ToolCallId,
	}
}

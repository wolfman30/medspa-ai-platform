package messaging

import "github.com/wolfman30/medspa-ai-platform/internal/conversation"

var _ conversationPublisher = (*conversation.Publisher)(nil)
var _ conversationStore = (*conversation.ConversationStore)(nil)
var _ OrgResolver = (*StaticOrgResolver)(nil)

package signature

import (
	"fmt"
	"strings"

	"flex-bbs/backend-go/bbs/types"
)

func CanonicalPostPayload(p *types.Post) string {
	var sb strings.Builder
	sb.WriteString("type=post\n")
	sb.WriteString(fmt.Sprintf("version=%d\n", p.Version))
	sb.WriteString("threadId=" + p.ThreadID + "\n")
	sb.WriteString("parentPostCid=" + strOrEmpty(p.ParentPostCID) + "\n")
	sb.WriteString("authorPubKey=" + p.AuthorPubKey + "\n")
	sb.WriteString("displayName=" + p.DisplayName + "\n")
	sb.WriteString("body.format=" + p.Body.Format + "\n")
	sb.WriteString("body.content=" + p.Body.Content + "\n")
	sb.WriteString("createdAt=" + p.CreatedAt)
	return sb.String()
}

func CanonicalBoardLogEntryPayload(e *types.BoardLogEntry) string {
	var sb strings.Builder
	sb.WriteString("type=boardLogEntry\n")
	sb.WriteString(fmt.Sprintf("version=%d\n", e.Version))
	sb.WriteString("boardId=" + e.BoardID + "\n")
	sb.WriteString("op=" + e.Op + "\n")
	sb.WriteString("threadId=" + e.ThreadID + "\n")
	sb.WriteString("postCid=" + strOrEmpty(e.PostCID) + "\n")
	sb.WriteString("oldPostCid=" + strOrEmpty(e.OldPostCID) + "\n")
	sb.WriteString("newPostCid=" + strOrEmpty(e.NewPostCID) + "\n")
	sb.WriteString("targetPostCid=" + strOrEmpty(e.TargetPostCID) + "\n")
	sb.WriteString("reason=" + strOrEmpty(e.Reason) + "\n")
	sb.WriteString("createdAt=" + e.CreatedAt + "\n")
	sb.WriteString("authorPubKey=" + e.AuthorPubKey + "\n")
	sb.WriteString("prevLogCid=" + strOrEmpty(e.PrevLogCID))
	return sb.String()
}

func CanonicalThreadMetaPayload(m *types.ThreadMeta) string {
	var sb strings.Builder
	sb.WriteString("type=threadMeta\n")
	sb.WriteString(fmt.Sprintf("version=%d\n", m.Version))
	sb.WriteString("boardId=" + m.BoardID + "\n")
	sb.WriteString("title=" + m.Title + "\n")
	sb.WriteString("createdAt=" + m.CreatedAt + "\n")
	sb.WriteString("createdBy=" + m.CreatedBy)
	return sb.String()
}

func CanonicalBoardMetaPayload(m *types.BoardMeta) string {
	var sb strings.Builder
	sb.WriteString("type=boardMeta\n")
	sb.WriteString(fmt.Sprintf("version=%d\n", m.Version))
	sb.WriteString("boardId=" + m.BoardID + "\n")
	sb.WriteString("title=" + m.Title + "\n")
	sb.WriteString("description=" + m.Description + "\n")
	sb.WriteString("createdAt=" + m.CreatedAt + "\n")
	sb.WriteString("createdBy=" + m.CreatedBy)
	return sb.String()
}

func strOrEmpty(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

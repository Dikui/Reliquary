package handler

import (
	werrors "reliquary/internal/errors"
	"strings"

	"github.com/gin-gonic/gin"
)

// queryFilterIDs accepts repeated parameters and comma-separated selections.
func queryFilterIDs(c *gin.Context, key string) ([]string, error) {
	var ids []string
	seen := map[string]bool{}
	for _, raw := range c.QueryArray(key) {
		for _, value := range strings.Split(raw, ",") {
			id := strings.TrimSpace(value)
			if id != "" && !seen[id] {
				ids = append(ids, id)
				seen[id] = true
			}
		}
	}
	if len(ids) > 100 {
		return nil, werrors.NewBadRequestError(key + " accepts at most 100 IDs")
	}
	return ids, nil
}

package helpers

import "github.com/google/uuid"

func UuidsToStrings(ids []uuid.UUID) []string {
	result := make([]string, 0, len(ids))

	for _, id := range ids {
		result = append(result, id.String())
	}

	return result
}

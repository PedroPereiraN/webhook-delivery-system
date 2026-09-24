package helpers

import (
	"database/sql"
	"fmt"
	"time"
)

func ParseSqlDate(value any) (time.Time, error) {
	// Layouts comuns que o PostgreSQL pode retornar
	layouts := []string{
		time.RFC3339,          // "2006-01-02T15:04:05Z07:00"
		"2006-01-02 15:04:05", // formato sem timezone
		"2006-01-02T15:04:05", // formato ISO sem timezone
		"2006-01-02",          // apenas data
	}

	var str string
	var valid bool

	switch v := value.(type) {
	case sql.NullString:
		if v.Valid {
			str = v.String
			valid = true
		}
	case sql.NullTime:
		if v.Valid {
			return v.Time, nil
		}
	case string:
		if v != "" {
			str = v
			valid = true
		}
	case *string:
		if v != nil && *v != "" {
			str = *v
			valid = true
		}
	default:
		return time.Time{}, fmt.Errorf("unsupported type %T in ParseSqlDate", value)
	}

	if !valid {
		return time.Time{}, nil
	}

	// Testa vários layouts conhecidos
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, str); err == nil {
			return parsed, nil
		}
	}

	return time.Time{}, fmt.Errorf("invalid date format: %v", str)
}

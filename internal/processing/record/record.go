package record

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/yourorg/reader/internal/domain"
)


func Hash(normalizedURL, username, password string) string {
	sum := sha256.Sum256([]byte(normalizedURL + "|" + username + "|" + password))
	return hex.EncodeToString(sum[:])
}

func Assemble(username, password, normalizedURL, domainPart, subdomain, suffix, port string, modifiedDate, createdAt time.Time) domain.ValidatedRecord {
	return domain.ValidatedRecord{
		RecordHash:   Hash(normalizedURL, username, password),
		Username:     username,
		Password:     password,
		URL:          normalizedURL,
		Domain:       domainPart,
		Subdomain:    subdomain,
		Suffix:       suffix,
		Port:         port,
		ModifiedDate: modifiedDate,
		CreatedAt:    createdAt,
	}
}

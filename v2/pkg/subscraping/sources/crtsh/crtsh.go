// Package crtsh logic
package crtsh

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	jsoniter "github.com/json-iterator/go"

	// postgres driver
	_ "github.com/lib/pq"

	"github.com/projectdiscovery/subfinder/v2/pkg/subscraping"
)

type subdomain struct {
	ID        int    `json:"id"`
	NameValue string `json:"name_value"`
}

// Source is the passive scraping agent
type Source struct {
	timeTaken time.Duration
}

// Run function returns all subdomains found with the service
func (s *Source) Run(ctx context.Context, domain string, session *subscraping.Session) <-chan subscraping.Result {
	results := make(chan subscraping.Result)
	startTime := time.Now()

	go func() {
		defer close(results)

		count := s.getSubdomainsFromSQL(domain, session, results)
		if count > 0 {
			return
		}
		_ = s.getSubdomainsFromHTTP(ctx, domain, session, results)
		s.timeTaken = time.Since(startTime)
	}()

	return results
}

func (s *Source) getSubdomainsFromSQL(domain string, session *subscraping.Session, results chan subscraping.Result) int {
	db, err := sql.Open("postgres", "host=crt.sh user=guest dbname=certwatch sslmode=disable binary_parameters=yes")
	if err != nil {
		results <- subscraping.Result{Source: s.Name(), Type: subscraping.Error, Error: err}
		return 0
	}

	defer db.Close()

	query := `WITH ci AS (
				SELECT min(sub.CERTIFICATE_ID) ID,
					min(sub.ISSUER_CA_ID) ISSUER_CA_ID,
					array_agg(DISTINCT sub.NAME_VALUE) NAME_VALUES,
					x509_commonName(sub.CERTIFICATE) COMMON_NAME,
					x509_notBefore(sub.CERTIFICATE) NOT_BEFORE,
					x509_notAfter(sub.CERTIFICATE) NOT_AFTER,
					encode(x509_serialNumber(sub.CERTIFICATE), 'hex') SERIAL_NUMBER
					FROM (SELECT *
							FROM certificate_and_identities cai
							WHERE plainto_tsquery('certwatch', $1) @@ identities(cai.CERTIFICATE)
								AND cai.NAME_VALUE ILIKE ('%' || $1 || '%')
							LIMIT 10000
						) sub
					GROUP BY sub.CERTIFICATE
			)
			SELECT array_to_string(ci.NAME_VALUES, chr(10)) NAME_VALUE
				FROM ci
						LEFT JOIN LATERAL (
							SELECT min(ctle.ENTRY_TIMESTAMP) ENTRY_TIMESTAMP
								FROM ct_log_entry ctle
								WHERE ctle.CERTIFICATE_ID = ci.ID
						) le ON TRUE,
					ca
				WHERE ci.ISSUER_CA_ID = ca.ID
				ORDER BY le.ENTRY_TIMESTAMP DESC NULLS LAST;`
	rows, err := db.Query(query, domain)
	if err != nil {
		results <- subscraping.Result{Source: s.Name(), Type: subscraping.Error, Error: err}
		return 0
	}
	if err := rows.Err(); err != nil {
		results <- subscraping.Result{Source: s.Name(), Type: subscraping.Error, Error: err}
		return 0
	}

	var count int
	var data string
	// Parse all the rows getting subdomains
	for rows.Next() {
		err := rows.Scan(&data)
		if err != nil {
			results <- subscraping.Result{Source: s.Name(), Type: subscraping.Error, Error: err}
			return count
		}

		count++
		for _, subdomain := range strings.Split(data, "\n") {
			value := session.Extractor.FindString(subdomain)
			if value != "" {
				results <- subscraping.Result{Source: s.Name(), Type: subscraping.Subdomain, Value: value}
			}
		}
	}
	return count
}

func (s *Source) getSubdomainsFromHTTP(ctx context.Context, domain string, session *subscraping.Session, results chan subscraping.Result) bool {
	resp, err := session.SimpleGet(ctx, fmt.Sprintf("https://crt.sh/?q=%%25.%s&output=json", domain))
	if err != nil {
		results <- subscraping.Result{Source: s.Name(), Type: subscraping.Error, Error: err}
		session.DiscardHTTPResponse(resp)
		return false
	}

	var subdomains []subdomain
	err = jsoniter.NewDecoder(resp.Body).Decode(&subdomains)
	if err != nil {
		results <- subscraping.Result{Source: s.Name(), Type: subscraping.Error, Error: err}
		resp.Body.Close()
		return false
	}

	resp.Body.Close()

	for _, subdomain := range subdomains {
		for _, sub := range strings.Split(subdomain.NameValue, "\n") {
			value := session.Extractor.FindString(sub)
			if value != "" {
				results <- subscraping.Result{Source: s.Name(), Type: subscraping.Subdomain, Value: value}
			}
		}
	}

	return true
}

// Name returns the name of the source
func (s *Source) Name() string {
	return "crtsh"
}

func (s *Source) IsDefault() bool {
	return true
}

func (s *Source) HasRecursiveSupport() bool {
	return true
}

func (s *Source) NeedsKey() bool {
	return false
}

func (s *Source) AddApiKeys(_ []string) {
	// no key needed
}

func (s *Source) TimeTaken() time.Duration {
	return s.timeTaken
}

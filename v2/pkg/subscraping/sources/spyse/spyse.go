// Package spyse logic
package spyse

import (
	"context"

	"github.com/projectdiscovery/subfinder/v2/pkg/subscraping"
	spyse "github.com/spyse-com/go-spyse/pkg"
)

const searchMethodResultsLimit = 10000

// Source is the passive scraping agent
type Source struct{}

// Run function returns all subdomains found with the service
func (s *Source) Run(ctx context.Context, domain string, session *subscraping.Session) <-chan subscraping.Result {
	results := make(chan subscraping.Result)

	go func() {
		defer close(results)

		if session.Keys.Spyse == "" {
			return
		}

		client, err := spyse.NewClient(session.Keys.Spyse, nil)
		if err != nil {
			results <- subscraping.Result{Source: s.Name(), Type: subscraping.Error, Error: err}
			return
		}

		domainSvc := spyse.NewDomainService(client)

		var searchDomain = "." + domain
		var subdomainsSearchParams spyse.QueryBuilder

		subdomainsSearchParams.AppendParam(spyse.QueryParam{
			Name:     domainSvc.Params().Name.Name,
			Operator: domainSvc.Params().Name.Operator.EndsWith,
			Value:    searchDomain,
		})

		totalResults, err := domainSvc.SearchCount(ctx, subdomainsSearchParams.Query)
		if err != nil {
			results <- subscraping.Result{Source: s.Name(), Type: subscraping.Error, Error: err}
			return
		}

		if totalResults == 0 {
			return
		}

		accountSvc := spyse.NewAccountService(client)

		quota, err := accountSvc.Quota(context.Background())
		if err != nil {
			results <- subscraping.Result{Source: s.Name(), Type: subscraping.Error, Error: err}
			return
		}

		// The default "Search" method returns only first 10 000 subdomains
		// To obtain more than 10 000 subdomains the "Scroll" method should be using
		// Note: The "Scroll" method is only available for "PRO" customers, so we need to check
		// quota.IsScrollSearchEnabled param
		if totalResults > searchMethodResultsLimit && quota.IsScrollSearchEnabled {
			var scrollResponse *spyse.DomainScrollResponse
			scrollResponse, err = domainSvc.ScrollSearch(
				ctx, subdomainsSearchParams.Query, "")
			if err != nil {
				results <- subscraping.Result{Source: s.Name(), Type: subscraping.Error, Error: err}
				return
			}

			for len(scrollResponse.Items) > 0 {
				scrollResponse, err = domainSvc.ScrollSearch(
					context.Background(), subdomainsSearchParams.Query, scrollResponse.SearchID)
				if err != nil {
					results <- subscraping.Result{Source: s.Name(), Type: subscraping.Error, Error: err}
					return
				}

				for i := range scrollResponse.Items {
					results <- subscraping.Result{Source: s.Name(), Type: subscraping.Subdomain, Value: scrollResponse.Items[i].Name}
				}
			}
		} else {
			var limit = 100
			var offset = 0
			var searchResults []spyse.Domain

			for ; int64(offset) < totalResults && int64(offset) < searchMethodResultsLimit; offset += limit {
				searchResults, err = domainSvc.Search(ctx, subdomainsSearchParams.Query, limit, offset)
				if err != nil {
					results <- subscraping.Result{Source: s.Name(), Type: subscraping.Error, Error: err}
					return
				}

				for i := range searchResults {
					results <- subscraping.Result{Source: s.Name(), Type: subscraping.Subdomain, Value: searchResults[i].Name}
				}
			}
		}
	}()

	return results
}

// Name returns the name of the source
func (s *Source) Name() string {
	return "spyse"
}

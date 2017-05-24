package uaatokenfetcher

import (
	"github.com/cloudfoundry-incubator/uaago"
	"github.com/cloudfoundry/gosteno"	
)

type UAATokenFetcher struct {
	uaaUrl                string
 	username              string
 	password              string
 	insecureSSLSkipVerify bool
	log		      *gosteno.Logger
}
 
func New(uaaToken string, logger *gosteno.Logger) *UAATokenFetcher {
	return &UAATokenFetcher{
 		uaaToken:              uaaToken,
 		log: logger,
	}
}

func (uaa *UAATokenFetcher) FetchAuthToken() string {
	return uaa.uaaToken
}

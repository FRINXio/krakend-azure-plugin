package main

import (
	 "context"
	 "fmt"
	 "github.com/dgrijalva/jwt-go"
	 "github.com/open-networks/go-msgraph"
	 _ "io/ioutil"
	 "net/http"
	 "os"
	 "strconv"
	 "strings"
	 "sync"
	 "time"
	 "unicode/utf8"
)

var HandlerRegisterer = registerer("krakend-azure-plugin")

type GroupMapping struct {
	sync.RWMutex
	groupMapping map[string]string
	queriedTenants map[string]time.Time
}

var groupMapping = &GroupMapping{}

type registerer string
var clientId string
var clientSecret string
var jwtHeaderName string
var jwtValuePrefix string
var groupUpdateIntervalMinutes float64
var groupTransformDisable string


func (r registerer) RegisterHandlers(f func(
	name string,
	handler func(
		context.Context,
		map[string]interface{},
		http.Handler) (http.Handler, error),
)) {
	f(string(r), r.registerHandlers)
}

func (r registerer) registerHandlers(ctx context.Context, extra map[string]interface{}, handler http.Handler) (http.Handler, error) {	

	handlerFunc :=  http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		jwtToken := req.Header.Get(jwtHeaderName)

		if jwtToken != "" {
			
			jwtToken = strings.ReplaceAll(jwtToken, " ", "")
			jwtToken = jwtToken[utf8.RuneCountInString(jwtValuePrefix):]

			claims := jwt.MapClaims{}

			// we don't check for err in ParseWithClaims, because err is always != nil when keyFunc
			// not provided (keyfunc needed only to verify signature, which we know is ok)
			jwt.ParseWithClaims(jwtToken, claims, nil)

			rolesValue := ""
			groupsValue := ""

			if claims["tid"] != nil {

				if claims["roles"] != nil {
					for _, role := range claims["roles"].([]interface{}) {
						if rolesValue == "" {
							rolesValue = rolesValue + role.(string)
						} else {
							rolesValue = rolesValue + "," + role.(string)
						}
					}
				}
				
				if groupTransformDisable != "true" {

					groupMapping.Lock()
					if val, ok := groupMapping.queriedTenants[claims["tid"].(string)]; !ok {
						updateTenantGroups(claims["tid"].(string))
					} else {
						if time.Now().Sub(val).Minutes() > groupUpdateIntervalMinutes {
							delete(groupMapping.queriedTenants, claims["tid"].(string)) // on the next request we will refresh tenant groups
						}
					}
					groupMapping.Unlock()
			
			
					groupMapping.RLock()
					if claims["groups"] != nil {
						for _, g := range claims["groups"].([]interface{}) {
							if val, ok := groupMapping.groupMapping[g.(string)]; ok {
								if groupsValue == "" {
									groupsValue = groupsValue + val
								} else {
									groupsValue = groupsValue + "," + val
								}
							}
						}
					}
					groupMapping.RUnlock()

				}

				req.Header.Add("x-tenant-id", strings.ReplaceAll(claims["tid"].(string), "-", "_") )

				if groupsValue != "" {
					req.Header.Add("x-auth-user-groups", groupsValue)
				}

				if rolesValue != "" {
					req.Header.Add("x-auth-user-roles", rolesValue)
				}
		
				var userIdentification string
		
				if claims["email"] != nil {
					userIdentification = claims["email"].(string)
				} else if claims["verified_primary_email"] != nil {
					userIdentification = claims["verified_primary_email"].(string)
				} else if claims["preferred_username"] != nil {
					userIdentification = claims["preferred_username"].(string)
				} else if claims["oid"] != nil {
					userIdentification = claims["oid"].(string)
				} else {
					userIdentification = "unknown"
				}
		
				req.Header.Add("from", userIdentification)

			}
		}

		handler.ServeHTTP(w, req)

	})

	return handlerFunc, nil
}

func updateTenantGroups(tenantId string) {

	graphClient, err := msgraph.NewGraphClient(tenantId, clientId, clientSecret)

	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: unable to connect to Azure AD (error: %v) tenant: %s \n", err, tenantId)
		return
	}

	groups, err := graphClient.ListGroups()

	if err != nil  {
		fmt.Fprintf(os.Stderr,"ERROR: unable to resolve groups (error: %v) for tenant: %s \n", err, tenantId)
		return
	}

	for _, g := range groups {
		groupMapping.groupMapping[g.ID] = g.DisplayName
	}

	groupMapping.queriedTenants[tenantId] = time.Now()
}

func init() {
	groupMapping.groupMapping = make(map[string]string)
	groupMapping.queriedTenants = make(map[string]time.Time)
	clientId = os.Getenv("AZURE_KRAKEND_PLUGIN_CLIENT_ID")
	clientSecret = os.Getenv("AZURE_KRAKEND_PLUGIN_CLIENT_SECRET")
	jwtHeaderName = os.Getenv("AZURE_KRAKEND_PLUGIN_JWT_HEADER_NAME")
	jwtValuePrefix = os.Getenv("AZURE_KRAKEND_PLUGIN_JWT_VALUE_PREFIX")
	groupUpdate := os.Getenv("AZURE_KRAKEND_PLUGIN_GROUP_UPDATE_IN_MINUTES")
	groupTransformDisable = os.Getenv("AZURE_KRAKEND_PLUGIN_GROUP_DISABLE")


	if jwtHeaderName == "" {
		jwtHeaderName = "Authorization"
		fmt.Fprintf(os.Stdout,"WARN: no JWT header name set, using default: %s \n", jwtHeaderName)
	}

	if groupUpdate == "" {
		groupUpdateIntervalMinutes = 120
		fmt.Fprintf(os.Stdout,"WARN: no Azure group update interval set, using default: %v minutes \n", groupUpdateIntervalMinutes)
	} else {
		var err error
		groupUpdateIntervalMinutes, err = strconv.ParseFloat(groupUpdate, 64)

		if err != nil  {
			groupUpdateIntervalMinutes = 120
			fmt.Fprintf(os.Stderr,"ERROR: unable to convert group refresh interval, using default: %v minutes \n", groupUpdateIntervalMinutes)
		}
	}

	if groupTransformDisable != "true" {

		if clientId == "" || clientSecret == "" {
			fmt.Fprintf(os.Stderr,"ERROR: Unable to retrieve plugin credentials: AZURE_KRAKEND_PLUGIN_CLIENT_ID or AZURE_KRAKEND_PLUGIN_CLIENT_SECRET missing \n")
		} else {
			fmt.Fprintf(os.Stdout,"INFO: azure-groups-plugin loaded successfully (JWT header name is \"%s\" and group refresh interval %v minutes ) \n", jwtHeaderName, groupUpdateIntervalMinutes)
		}

	} else {
		fmt.Fprintf(os.Stdout,"INFO: group transformation is disabled")
	}
}

func main() {

}

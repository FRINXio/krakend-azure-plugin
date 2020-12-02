package main

import (
	"context"
	"fmt"
	"github.com/dgrijalva/jwt-go"
	"github.com/open-networks/go-msgraph"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

var ClientRegisterer = registerer("krakend-azure-plugin")
var groupMapping = make(map[string]string)
var queriedTenants = make(map[string]time.Time)
type registerer string
var clientId string
var clientSecret string
var jwtHeaderName string
var jwtValuePrefix string
var groupUpdateIntervalMinutes float64

func (r registerer) RegisterClients(f func(
	name string,
	handler func(context.Context, map[string]interface{}) (http.Handler, error),
)) {
	f(string(r), r.registerClients)
}

func (r registerer) registerClients(ctx context.Context, extra map[string]interface{}) (http.Handler, error) {
	handlerFunc := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		jwtToken := req.Header.Get(jwtHeaderName)

		if jwtToken == "" {
			return
		}

		jwtToken = jwtToken[len(jwtValuePrefix):]

		claims := jwt.MapClaims{}

		// we don't check for err in ParseWithClaims, because err is always != nil when keyFunc
		// not provided (keyfunc needed only to verify signature, which we know is ok)
		jwt.ParseWithClaims(jwtToken, claims, nil)

		if val, ok := queriedTenants[claims["tid"].(string)]; !ok {
			updateTenantGroups(claims["tid"].(string))
		} else {
			if time.Now().Sub(val).Minutes() > groupUpdateIntervalMinutes {
				delete(queriedTenants, claims["tid"].(string)) // on the next request we will refresh tenant groups
			}
		}

		groupsValue := ""

		if claims["groups"] != nil {
			for _, g := range claims["groups"].([]interface{}) {
				if val, ok := groupMapping[g.(string)]; ok {
					if groupsValue == "" {
						groupsValue = groupsValue + val
					} else {
						groupsValue = groupsValue + ", " + val
					}
				}
			}
		}

		req.Header.Add("x-tenant-id", strings.ReplaceAll(claims["tid"].(string), "-", "_") )

		if groupsValue != "" {
			req.Header.Add("x-auth-user-groups", groupsValue)
		}

		var userIdentification string

		if claims["email"] != nil {
			userIdentification = claims["email"].(string)
		} else if claims["verified_primary_email"] != nil {
			userIdentification = claims["verified_primary_email"].(string)
		} else if claims["oid"] != nil {
			userIdentification = claims["oid"].(string)
		} else {
			userIdentification = "unknown"
		}

		req.Header.Add("from", userIdentification)

		response, err := http.DefaultClient.Do(req)

		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: unable to connect to backend, error: %v \n", err)
			w.WriteHeader(500)
			return
		}

		defer response.Body.Close()

		bytes, err := ioutil.ReadAll(response.Body)

		if err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: unable to process response from backend, error: %v \n", err)
		}

		w.WriteHeader(response.StatusCode)
		bytesWritten, err := w.Write(bytes)

		if bytesWritten != len(bytes) || err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: unable to read the response from backend, error: %v bytes from backend %d bytes written %d \n", err, len(bytes), bytesWritten)
		}

		for key, vals := range response.Header {
			for _, val := range vals {
				w.Header().Add(key, val)
			}
		}
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
		groupMapping[g.ID] = g.DisplayName
	}

	queriedTenants[tenantId] = time.Now()
}

func init() {
	clientId = os.Getenv("AZURE_KRAKEND_PLUGIN_CLIENT_ID")
	clientSecret = os.Getenv("AZURE_KRAKEND_PLUGIN_CLIENT_SECRET")
	jwtHeaderName = os.Getenv("AZURE_KRAKEND_PLUGIN_JWT_HEADER_NAME")
	jwtValuePrefix = os.Getenv("AZURE_KRAKEND_PLUGIN_JWT_VALUE_PREFIX")
	groupUpdate := os.Getenv("AZURE_KRAKEND_PLUGIN_GROUP_UPDATE_IN_MINUTES")

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

	if clientId == "" || clientSecret == "" {
		fmt.Fprintf(os.Stderr,"ERROR: Unable to retrieve plugin credentials: AZURE_KRAKEND_PLUGIN_CLIENT_ID or AZURE_KRAKEND_PLUGIN_CLIENT_SECRET missing \n")
	} else {
		fmt.Fprintf(os.Stdout,"INFO: azure-groups-plugin loaded successfully (JWT header name is \"%s\" and group refresh interval %v minutes ) \n", jwtHeaderName, groupUpdateIntervalMinutes)
	}
}

func main() {

}

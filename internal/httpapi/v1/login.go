package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/bureau14/qdb-api-rest/internal/observe"
)

// v1Token is the old server's login answer.
type v1Token struct {
	Token string `json:"token"`
}

// v2LoginPath is the v2 login endpoint that the v1 login calls.
const v2LoginPath = "/api/v2/auth/login"

// loginHandler answers the old server's login through the v2 login. It
// answers 200 {"token"} with the v2 access token, and 401 {"message"}
// for every other answer the v2 login gives. route is the path the
// request duration is recorded under.
func loginHandler(v2 http.Handler, route string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. build the v2 login request from the v1 one
		// 2. run it through the v2 router in process and record its answer
		// 3. convert the answer: 200 to the token, anything else to 401
		// 4. record the duration under the v1 route
		start := time.Now()
		ctx := r.Context()

		// 1. The inner request carries the outer context, because the v2
		//    handler reads the logger, the cluster and the keychain from it.
		in, err := http.NewRequestWithContext(ctx, http.MethodPost, v2LoginPath, r.Body)
		if err != nil {
			// Unreachable, because the method and the path are constants.
			writeV1JSON(w, http.StatusUnauthorized, v1Error{Message: err.Error()})
			return
		}
		// 1. Content-Type is application/json whatever the client sent,
		//    because the v2 login refuses anything else with 415 and the v1
		//    login has no 415. The inner request carries no Accept-Encoding,
		//    so the v2 answer comes back uncompressed.
		in.Header.Set("Content-Type", "application/json")

		// 2. The answer is recorded whole, because a login answer is a few
		//    hundred bytes.
		rec := httptest.NewRecorder()
		v2.ServeHTTP(rec, in)

		// 3. On 200 the refresh token is dropped, because a v1 client has
		//    no refresh endpoint. Any other status, or a body that does not
		//    decode, is 401, because the old server answered 401 for every
		//    login it could not complete.
		status := http.StatusUnauthorized
		var grant struct {
			AccessToken string `json:"access_token"`
		}
		var problem struct {
			Detail string `json:"detail"`
		}
		switch {
		case rec.Code != http.StatusOK:
			if json.Unmarshal(rec.Body.Bytes(), &problem) != nil || problem.Detail == "" {
				problem.Detail = http.StatusText(rec.Code)
			}
			writeV1JSON(w, status, v1Error{Message: problem.Detail})
		case json.Unmarshal(rec.Body.Bytes(), &grant) != nil:
			writeV1JSON(w, status, v1Error{Message: "the login answered an undecodable grant"})
		default:
			status = http.StatusOK
			writeV1JSON(w, status, v1Token{Token: grant.AccessToken})
		}

		// 4. The v2 login records under its own route, so one v1 request
		//    counts once under each.
		observe.MetricsFrom(ctx).ObserveRequest(http.MethodPost, route, status, time.Since(start))
	})
}

# mockcas
Mock CAS server and client for testing [CAS Protocol 3.0](https://apereo.github.io/cas/7.3.x/protocol/CAS-Protocol-Specification.html).

## example
### static auth
Start mockcas and visit http://192.168.1.2:9090/app/login in browser. Login with default username `casuser` and password `Mellon`.  
```sh
go run ./cmd/mockcas
# 2026-01-02 00:09:12 [I] using cas server url prefix:  http://192.168.1.2:9090/cas
# 2026-01-02 00:09:12 [I] using cas client service url: http://192.168.1.2:9090/app/validate
# 2026-01-02 00:09:12 [I] using cas client logout url:  http://192.168.1.2:9090/app/logout
# 2026-01-02 00:09:12 [I] service started: http://0.0.0.0:9090
```
### ldap auth
Start [lldap](https://github.com/lldap/lldap) as authentication source and visit http://192.168.1.2:17170 to manage users.  
Start mockcas and visit http://192.168.1.2:9090/app/login in browser. Login with default username `admin` and password `P@ssw0rd`.  
```sh
docker volume create lldap_data
docker run --rm \
  --name lldap \
  -p 3890:3890 \
  -p 17170:17170 \
  -e TZ=Asia/Shanghai \
  -e LLDAP_JWT_SECRET=zKwp1V5qw9A02cCoQXQgf5BKvWTnbfpt \
  -e LLDAP_KEY_SEED=e131dsG6gdcj1fYG9EH4PbVAAPWYd77A \
  -e LLDAP_LDAP_BASE_DN=dc=example,dc=com \
  -e LLDAP_LDAP_USER_PASS=P@ssw0rd \
  -v lldap_data:/data \
  lldap/lldap:2026-01-06

go run ./cmd/mockcas \
  -cas-auth-method ldap \
  -ldap-server-url "ldap://127.0.0.1:3890" \
  -ldap-bind-dn "cn=admin,ou=people,dc=example,dc=com" \
  -ldap-bind-pass "P@ssw0rd" \
  -ldap-base-dn "ou=people,dc=example,dc=com" \
  -ldap-search-filter "(uid=%s)"
```

## usage
```
  -help                   bool     Show usage message and quit
  -config                 string   Specify file path of custom configuration json
  -d                      bool     Enable debug output [CFG_DEBUG]
  -l                      string   Server listen addr [CFG_LISTEN_ADDR] (default "0.0.0.0:9090")
  -cas-server-url-prefix  string   URL prefix of the CAS server, auto detected if empty [CFG_CAS_SERVER_URL_PREFIX]
  -cas-client-service-url string   Service URL of the CAS client application, auto detected if empty [CFG_CAS_CLIENT_SERVICE_URL]
  -cas-client-logout-url  string   Logout URL of the CAS client application, auto detected if empty [CFG_CAS_CLIENT_LOGOUT_URL]
  -cas-auth-method        string   Authentication method of the CAS server, static or ldap [CFG_CAS_AUTH_METHOD] (default "static")
  -ldap-server-url        string   URL of the LDAP server [CFG_LDAP_SERVER_URL] (default "ldap://127.0.0.1:3890")
  -ldap-bind-dn           string   DN to bind to the LDAP server [CFG_LDAP_BIND_DN] (default "cn=admin,ou=people,dc=example,dc=com")
  -ldap-bind-pass         string   Password for the LDAP bind DN [CFG_LDAP_BIND_PASS] (default "password")
  -ldap-base-dn           string   Base DN for LDAP search [CFG_LDAP_BASE_DN] (default "ou=people,dc=example,dc=com")
  -ldap-search-filter     string   Filter for LDAP search [CFG_LDAP_SEARCH_FILTER] (default "(uid=%s)")
```

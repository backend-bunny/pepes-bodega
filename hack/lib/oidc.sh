# shellcheck shell=bash
# Scripted Dex password login through the API's real OIDC flow.
#
#   oidc_login SERVER USER PASSWORD COOKIE_JAR [PUBLIC=LOCAL ...]
#
# Follows the redirect chain by hand. Each PUBLIC=LOCAL pair rewrites redirects
# to a public origin onto a local one, e.g. when the public URLs are
# netbird-exposed and can't be reached from the exposing host itself (421).
# The OIDC parameters (code, state, redirect_uri) are untouched, so the API's
# code exchange still matches what Dex issued. Leaves a session for SERVER in
# COOKIE_JAR.
oidc_login() {
  local server=$1 user=$2 pass=$3 jar=$4; shift 4
  local maps=("$@") url="$server/auth/login" data=() hops

  for hops in $(seq 1 20); do
    local out code loc
    out=$(curl -sS -c "$jar" -b "$jar" -o /dev/null -w '%{http_code} %{redirect_url}' "${data[@]}" "$url") || return 1
    data=()
    code=${out%% *} loc=${out#* }
    if [[ $code == 3* && -n $loc ]]; then
      url=$(_oidc_map "$loc" "${maps[@]}")
      continue
    fi
    # Dex's password form (single local connector): submit credentials once.
    if [[ $code == 200 && $url == */auth/local/login* && $user != "" ]]; then
      data=(--data-urlencode "login=$user" --data-urlencode "password=$pass")
      user=""  # never resubmit: a second form means the password was rejected
      continue
    fi
    break
  done

  curl -sS -b "$jar" -f -o /dev/null "$server/api/v1/me" || {
    echo "OIDC login via $server failed (stopped at $url)" >&2
    return 1
  }
}

# _oidc_map URL [PUBLIC=LOCAL ...] -> URL with the first matching origin replaced
_oidc_map() {
  local url=$1 m; shift
  for m in "$@"; do
    if [[ $url == "${m%%=*}"* ]]; then
      echo "${m#*=}${url#"${m%%=*}"}"
      return
    fi
  done
  echo "$url"
}

# oidc_token SERVER COOKIE_JAR NAME -> prints a new pbt_ token
oidc_token() {
  local server=$1 jar=$2 name=$3
  curl -sS -b "$jar" -f -H 'Content-Type: application/json' \
    -d "{\"name\":\"$name\"}" "$server/api/v1/tokens" | jq -er .secret
}

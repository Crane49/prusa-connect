#!/usr/bin/env bash
#
# Exercise compatibility with published core releases.
#
# Historical releases can prove that HEAD opens and migrates their data even
# when they predate automatic upgrades. A release can exercise the replacement
# path only when it publishes the binary, SHA256SUMS and SHA256SUMS.sig and its
# binary reports upgrade metadata.
set -euo pipefail

repository="${GITHUB_REPOSITORY:-prusa3d/prusa-connect}"
binary_name="${UPGRADE_BINARY_NAME:-prusa_connect_amd64}"
mode="${1:-}"
head_binary="${HEAD_BINARY:-}"
work_root="${UPGRADE_COMPAT_WORK_ROOT:-${RUNNER_TEMP:-${TMPDIR:-/tmp}}/pcl-upgrade-compat}"
summary="${GITHUB_STEP_SUMMARY:-/dev/null}"
server_url="http://127.0.0.1:18080"
release_limit="${UPGRADE_COMPAT_RELEASE_LIMIT:-0}"

notice() {
  printf '%s\n' "$*"
  if [ "$summary" != /dev/null ]; then
    printf '%s\n' "$*" >> "$summary"
  fi
}

die() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

require() {
  command -v "$1" >/dev/null 2>&1 || die "$1 is required"
}

release_json() {
  gh api --paginate "repos/$repository/releases?per_page=100"
}

published_rows() {
  jq -r --arg binary "$binary_name" '
    .[]
    | select(.draft == false and (.tag_name | startswith("v")))
    | (.assets | map({key: .name, value: .url}) | from_entries) as $assets
    | select($assets[$binary] != null and $assets["config.yaml"] != null)
    | [.tag_name, $assets[$binary], $assets["config.yaml"]]
    | @tsv
  '
}

signed_rows() {
  jq -r --arg binary "$binary_name" '
    .[]
    | select(.draft == false and (.tag_name | startswith("v")))
    | (.assets | map({key: .name, value: .url}) | from_entries) as $assets
    | select(
        $assets[$binary] != null
        and $assets["config.yaml"] != null
        and $assets["SHA256SUMS"] != null
        and $assets["SHA256SUMS.sig"] != null
      )
    | [
        .tag_name,
        $assets[$binary],
        $assets["config.yaml"],
        $assets["SHA256SUMS"],
        $assets["SHA256SUMS.sig"]
      ]
    | @tsv
  '
}

# stage_body prints the JSON that stages a binary: version, binary, sums, signature.
# The files go in base64 through --rawfile, because a binary is far longer than
# the kernel lets one argument be.
stage_body() {
  local dir
  dir="$(mktemp -d)"
  base64 <"$2" | tr -d '\n' >"$dir/binary"
  base64 <"$3" | tr -d '\n' >"$dir/sums"
  base64 <"$4" | tr -d '\n' >"$dir/signature"
  jq -n \
    --arg version "$1" \
    --arg name "$binary_name" \
    --rawfile binary "$dir/binary" \
    --rawfile sums "$dir/sums" \
    --rawfile signature "$dir/signature" \
    '{version:$version,name:$name,binary:$binary,sha256sums:$sums,signature:$signature}'
  rm -rf "$dir"
}

download() {
  local url="$1"
  local destination="$2"
  gh api -H 'Accept: application/octet-stream' "$url" > "$destination"
}

wait_ready() {
  local binary="$1"
  local config="$2"
  local pid="$3"
  local log="$4"
  local attempt

  for attempt in $(seq 1 90); do
    if ! kill -0 "$pid" 2>/dev/null; then
      wait "$pid" || true
      cat "$log" >&2
      die "$binary exited before it became ready"
    fi
    if "$binary" core -config "$config" -addr 127.0.0.1:18080 healthcheck >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done

  cat "$log" >&2
  die "$binary did not become ready"
}

stop_server() {
  local pid="$1"
  local log="$2"

  kill -TERM "$pid" 2>/dev/null || true
  for _ in $(seq 1 30); do
    if ! kill -0 "$pid" 2>/dev/null; then
      wait "$pid" || true
      return 0
    fi
    sleep 1
  done
  cat "$log" >&2
  kill -KILL "$pid" 2>/dev/null || true
  wait "$pid" || true
  die "server did not stop after SIGTERM"
}

cleanup_pid_file() {
  local pid_file="$1"
  [ -s "$pid_file" ] || return 0

  local pid
  pid="$(cat "$pid_file")"
  if kill -0 "$pid" 2>/dev/null; then
    kill -TERM "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
}

run_server() {
  local binary="$1"
  local config="$2"
  local log="$3"
  local pid_file="$4"

  "$binary" core -config "$config" -addr 127.0.0.1:18080 >"$log" 2>&1 &
  local pid=$!
  printf '%s\n' "$pid" > "$pid_file"
  wait_ready "$binary" "$config" "$pid" "$log"
}

discover() {
  require gh
  require jq

  local releases candidate tag binary_url config_url sums_url signature_url
  releases="$(release_json)"
  candidate="$(printf '%s\n' "$releases" | signed_rows | sed -n '1p')"
  if [ -z "$candidate" ]; then
    notice "### Automatic upgrade compatibility"
    notice
    notice "Skipped: no published release has a platform binary, SHA256SUMS and SHA256SUMS.sig yet."
    notice "Historical data migration still runs in its own job."
    if [ -n "${GITHUB_OUTPUT:-}" ]; then
      {
        echo "available=false"
        echo "reason=no signed auto-upgrade-capable release is published"
      } >> "$GITHUB_OUTPUT"
    fi
    return 0
  fi

  IFS=$'\t' read -r tag binary_url config_url sums_url signature_url <<<"$candidate"
  mkdir -p "$work_root/discovery"
  download "$binary_url" "$work_root/discovery/$binary_name"
  chmod +x "$work_root/discovery/$binary_name"

  if ! "$work_root/discovery/$binary_name" -version -json >"$work_root/discovery/version.json" 2>/dev/null ||
    ! jq -e '.host_schema_version != null and .host_schema_compat != null' \
      "$work_root/discovery/version.json" >/dev/null; then
    notice "### Automatic upgrade compatibility"
    notice
    notice "Skipped: $tag is signed, but its binary does not report the schema metadata required by automatic upgrades."
    if [ -n "${GITHUB_OUTPUT:-}" ]; then
      {
        echo "available=false"
        echo "reason=the latest signed release predates automatic upgrade metadata"
      } >> "$GITHUB_OUTPUT"
    fi
    return 0
  fi

  notice "Automatic upgrade source: $tag"
  if [ -n "${GITHUB_OUTPUT:-}" ]; then
    {
      echo "available=true"
      echo "tag=$tag"
      echo "binary_url=$binary_url"
      echo "config_url=$config_url"
      echo "sums_url=$sums_url"
      echo "signature_url=$signature_url"
    } >> "$GITHUB_OUTPUT"
  fi
}

historical_data() {
  require gh
  require jq
  [ -x "$head_binary" ] || die "HEAD_BINARY must name an executable HEAD binary"

  local releases rows tested=0
  releases="$(release_json)"
  rows="$(printf '%s\n' "$releases" | published_rows)"
  if [ "$release_limit" -gt 0 ] 2>/dev/null; then
    rows="$(printf '%s\n' "$rows" | sed -n "1,${release_limit}p")"
  elif [ "$release_limit" != 0 ]; then
    die "UPGRADE_COMPAT_RELEASE_LIMIT must be zero or a positive integer"
  fi
  [ -n "$rows" ] || die "no published release has $binary_name and config.yaml"

  notice "### Historical data compatibility"
  while IFS=$'\t' read -r tag binary_url config_url; do
    local case_dir old_binary config old_log old_pid head_log head_pid
    case_dir="$work_root/data/${tag#v}"
    old_binary="$case_dir/$binary_name"
    config="$case_dir/config/config.yaml"
    old_log="$case_dir/old.log"
    old_pid="$case_dir/old.pid"
    head_log="$case_dir/head.log"
    head_pid="$case_dir/head.pid"
    mkdir -p "$case_dir/config"

    download "$binary_url" "$old_binary"
    download "$config_url" "$config"
    chmod +x "$old_binary"

    (
      cd "$case_dir"
      trap 'cleanup_pid_file "$old_pid"; cleanup_pid_file "$head_pid"' EXIT
      run_server "$old_binary" "$config" "$old_log" "$old_pid"
      stop_server "$(cat "$old_pid")" "$old_log"

      test -s config/config.d/authz.db ||
        die "$tag created no host database at config/config.d/authz.db"
      before="$(shasum -a 256 config/config.d/authz.db | awk '{print $1}')"

      run_server "$head_binary" "$config" "$head_log" "$head_pid"
      "$head_binary" -version -json |
        jq -e '.host_schema_version >= .host_schema_compat' >/dev/null
      stop_server "$(cat "$head_pid")" "$head_log"

      after="$(shasum -a 256 config/config.d/authz.db | awk '{print $1}')"
      printf '%s  before\n%s  after\n' "$before" "$after" > migration-sha256.txt
    )

    notice "- $tag: published binary created the data; HEAD opened and migrated it"
    tested=$((tested + 1))
  done <<<"$rows"

  notice
  notice "Checked $tested published core releases."
}

self_upgrade() {
  require curl
  require jq
  require shasum
  [ -x "$head_binary" ] || die "HEAD_BINARY must name an executable HEAD binary"
  [ -n "${SOURCE_TAG:-}" ] || die "SOURCE_TAG is required"
  [ -n "${SOURCE_BINARY_URL:-}" ] || die "SOURCE_BINARY_URL is required"
  [ -n "${SOURCE_SUMS_URL:-}" ] || die "SOURCE_SUMS_URL is required"
  [ -n "${SOURCE_SIGNATURE_URL:-}" ] || die "SOURCE_SIGNATURE_URL is required"
  [ -n "${FIXTURE_PUBLIC_KEY:-}" ] || die "FIXTURE_PUBLIC_KEY is required"
  [ -n "${FIXTURE_KEY_ID:-}" ] || die "FIXTURE_KEY_ID is required"
  [ -n "${CANDIDATE_VERSION:-}" ] || die "CANDIDATE_VERSION is required"

  local case_dir old_binary config log pid_file pid
  case_dir="$work_root/self"
  old_binary="$case_dir/$binary_name"
  config="$case_dir/config/config.yaml"
  log="$case_dir/server.log"
  pid_file="$case_dir/server.pid"
  mkdir -p "$case_dir/config" "$case_dir/storage"
  download "$SOURCE_BINARY_URL" "$old_binary"
  download "$SOURCE_SUMS_URL" "$case_dir/source-SHA256SUMS"
  download "$SOURCE_SIGNATURE_URL" "$case_dir/source-SHA256SUMS.sig"
  chmod +x "$old_binary"

  cat >"$config" <<EOF
persistence:
  type: sqlite
core:
  api:
    addr: "127.0.0.1:18080"
  frontend:
    enabled: false
  principal: operator
  auth:
    local: true
  upgrade:
    trusted_keys:
      - id: "$FIXTURE_KEY_ID"
        key: "$FIXTURE_PUBLIC_KEY"
resources:
  storage:
    local:
      type: filesystem
      enabled: true
      config:
        root: ./storage
EOF

  (
    cd "$case_dir"
    trap 'cleanup_pid_file "$pid_file"' EXIT
    password="upgrade-compatibility-password"
    "$old_binary" admin create -config "$config" \
      -name upgrade-ci -password "$password" > admin.txt
    run_server "$old_binary" "$config" "$log" "$pid_file"
    pid="$(cat "$pid_file")"
    curl --fail --silent --show-error -c cookies.txt \
      -H 'Content-Type: application/json' \
      --data "{\"username\":\"upgrade-ci\",\"password\":\"$password\"}" \
      "$server_url/api/v1/auth/login" > login.json

    stage_body "${SOURCE_TAG#v}" "$old_binary" source-SHA256SUMS source-SHA256SUMS.sig \
      > source-stage.json
    curl --fail --silent --show-error -b cookies.txt -H 'Content-Type: application/json' \
      --data-binary @source-stage.json \
      "$server_url/api/v1/server/upgrade/staged" > source-verified.json
    jq -e '.state == "verified" and .verified.key_id != ""' source-verified.json >/dev/null
    curl --fail --silent --show-error -b cookies.txt -X DELETE \
      "$server_url/api/v1/server/upgrade" >/dev/null

    stage_body "$CANDIDATE_VERSION" "$head_binary" "$CANDIDATE_SUMS" "$CANDIDATE_SIGNATURE" \
      > stage.json

    curl --fail --silent --show-error -b cookies.txt -H 'Content-Type: application/json' \
      --data-binary @stage.json \
      "$server_url/api/v1/server/upgrade/staged" > staged.json
    jq -e --arg version "$CANDIDATE_VERSION" \
      '.to_version == $version and .state == "verified" and .verified.key_id != ""' \
      staged.json >/dev/null

    curl --fail --silent --show-error -b cookies.txt -X POST \
      "$server_url/api/v1/server/upgrade" > prepared.json
    jq -e '.state == "rehearsed"' prepared.json >/dev/null

    curl --fail --silent --show-error -b cookies.txt -H 'Content-Type: application/json' \
      --data '{"schedule":"now"}' \
      "$server_url/api/v1/server/upgrade/apply" > applied.json

    for _ in $(seq 1 120); do
      if version="$("$old_binary" -version -json 2>/dev/null | jq -r '.version // empty')" &&
        [ "$version" = "$CANDIDATE_VERSION" ] &&
        "$old_binary" core -config "$config" -addr 127.0.0.1:18080 healthcheck >/dev/null 2>&1; then
        break
      fi
      sleep 1
    done

    version="$("$old_binary" -version -json | jq -r '.version')"
    [ "$version" = "$CANDIDATE_VERSION" ] ||
      die "$SOURCE_TAG left $version installed, expected $CANDIDATE_VERSION"
    curl --fail --silent --show-error -b cookies.txt \
      "$server_url/api/v1/server/upgrade/history" |
      jq -e --arg version "$CANDIDATE_VERSION" \
        'any(.[]; .to_version == $version and .state == "committed")' >/dev/null

    pid="$(cat "$pid_file")"
    stop_server "$pid" "$log"
  )

  notice "### Automatic upgrade compatibility"
  notice
  notice "$SOURCE_TAG verified the signed HEAD candidate, rehearsed its migrations, replaced itself and committed $CANDIDATE_VERSION."
}

case "$mode" in
  discover) discover ;;
  historical-data) historical_data ;;
  self-upgrade) self_upgrade ;;
  *) die "usage: $0 discover|historical-data|self-upgrade" ;;
esac

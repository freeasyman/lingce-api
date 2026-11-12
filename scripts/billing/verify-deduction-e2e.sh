#!/usr/bin/env bash
# =============================================================================
# 六产品真实扣量验证（可重跑回归脚本）
# =============================================================================
#
# 验证「真调产品生成接口 → 额度是否按规则扣减」这条链路。六个产品与规则：
#
#   听写病历 tingxie : 一份病历草稿  → record 1 + hour = 录音秒数/3600（2 位小数）
#   赢客专家 yingke  : 一次成交分析  → hour = 录音秒数/3600
#   合规卫士 hegui   : 一次沟通检查  → hour = 录音秒数/3600
#   随访顾问 suifang : 一次随访分析  → hour = 录音秒数/3600
#                                     （交付 0 条任务时不扣，脚本按空交付判定）
#   客户总监 kehu    : 一次截图分析  → shot 1
#   获客先锋 huoke   : 一次文稿保存  → record 1
#
# -----------------------------------------------------------------------------
# 用法
# -----------------------------------------------------------------------------
#   bash scripts/billing/verify-deduction-e2e.sh                 # 默认 --check-only
#   bash scripts/billing/verify-deduction-e2e.sh --check-only    # 只读检查，免费
#   CONFIRM_SPEND=YES bash scripts/billing/verify-deduction-e2e.sh --real
#   bash scripts/billing/verify-deduction-e2e.sh --cleanup       # 清理上次 --real 的产物
#   bash scripts/billing/verify-deduction-e2e.sh --real --only tingxie,huoke
#   CONFIRM_SPEND=YES bash scripts/billing/verify-deduction-e2e.sh --real --fresh-data
#
#   --check-only   （默认，免费）只读检查：额度行存在、余额 = granted + rollover - used、
#                  流水与幂等键结构、六个生成接口可达（HEAD）、目标数据可用性。
#                  不调模型、不写业务数据、不花钱。
#   --real         （花钱）真调六个产品的生成接口，逐项核对扣量。必须显式指定，
#                  且需要 CONFIRM_SPEND=YES。带预算上限（BUDGET_CNY，默认 10 元）：
#                  跑前记录基线，每完成一个产品就从 gateway_call_records 汇总窗口花费，
#                  超限立即中止（退出码 2）。
#   --keep-data    （默认行为）跑完不删数据，业务方可逐个产品查看。可不加。
#   --cleanup      删除本次（或上次）--real 生成的病历/分析/任务/文稿/截图记录，
#                  不动额度流水（entitlement_ledger 只读不删）。可单独使用。
#   --fresh-data   忽略上次选择的验证目标，重新挑一条「未扣过」的数据。
#                  默认会复用上次目标，因此第二次 --real 会全部命中幂等、不产生新费用。
#   --only LIST    只跑指定产品，逗号分隔：tingxie,yingke,hegui,suifang,kehu,huoke。
#
# -----------------------------------------------------------------------------
# 花费警告
# -----------------------------------------------------------------------------
#   --real 会真实调用六个产品，从而真实调用大模型网关并产生费用（参考量级：
#   一次全量约 ¥0.5~¥2，取决于模型与转写长度）。请确认：
#     1. 目标是 dev / test 库，不是生产；
#     2. BUDGET_CNY 设置符合预期（默认 10 元）；
#     3. 已设置 CONFIRM_SPEND=YES。
#   脚本检测到生产库会直接退出；--check-only 永远不调用模型。
#
# -----------------------------------------------------------------------------
# 数据准备（不写死 id，每次都从库里挑）
# -----------------------------------------------------------------------------
#   目标租户     : TENANT_ID（默认 5001 灵策综合医院）
#   听写病历录音 : recordings 中「医生有医疗科室 + cleaned_transcription 非空 + 时长 60~3600s
#                  + 该接诊没有已扣过费病历」的录音，按时长倒序取第一条
#   赢客/随访    : recordings 中「有转写 + 时长 300~3600s + 该产品幂等键未落账」的录音
#                  随访还要满足 followup.Service.validateReferences 的全部引用关系
#   合规录音     : recordings 中「120~900s + 客户/员工/接诊齐全 + 未检查过」的录音
#   截图         : patient_conversation_sources 中任一租户已分析成功的 data:image 截图；
#                  通过内部接口在目标租户下再分析一次（本租户 content_hash 已存在则复用）
#   选题         : content_topics 中目标租户的选题；没有时 --real 自动新建一条
#                  「[计费回归] 文稿保存扣量验证」选题（--cleanup 会一并删除）
#   查不到就 SKIP（不算 FAIL）；挑到的目标写入 STATE_FILE，重复跑据此命中幂等。
#
#   截图借用其他租户的原因：默认租户 5001 没有历史截图，而截图分析接口只接受
#   image_data（data URL），因此从库里有截图的租户借一张图，在目标租户下重建分析。
#
# -----------------------------------------------------------------------------
# 环境变量
# -----------------------------------------------------------------------------
#   API_BASE       默认 http://127.0.0.1:18080
#   ADMIN_JWT      运营 admin token，或一个「内容是 token 的文件路径」（huoke 保存文稿用）
#   INTERNAL_TOKEN 内部接口 token（六个产品里除 huoke 外都用它）；缺省时尝试从
#                  configs/secrets.local.toml 的 external.internal_worker_token 读取
#   DB_DSN         dev 库连接串；缺省时尝试用 configs/dev.toml + secrets 合成
#   TENANT_ID      默认 5001（灵策综合医院）
#   BUDGET_CNY     默认 10（元）
#   STATE_FILE     幂等目标状态文件，默认
#                  ${XDG_STATE_HOME:-$HOME/.local/state}/lingce-api/verify-deduction-e2e/tenant-<id>.json
#                  （记录上次用过的录音/截图/文稿 id，保证重复跑命中幂等）
#   CONFIRM_SPEND  必须等于 YES 才允许 --real
#   HTTP_TIMEOUT   单次 HTTP 超时秒数，默认 300
#   POLL_TIMEOUT   异步分析轮询超时秒数，默认 240
#   E2E_ALLOW_NON_DEV=1  允许在库名不含 dev/test/local/stage 的库上运行（不建议）
#
# -----------------------------------------------------------------------------
# 幂等与重复跑
# -----------------------------------------------------------------------------
#   六个产品都按业务幂等键扣量：
#     emr:record:<record_id>:draft
#     deal:recording:<recording_id>:analysis
#     compliance:run:<run_id>:check
#     followup:recording:<recording_id>:analysis
#     patientops:screenshot:<source_id>:review
#     content:article:<article_id>:deliver
#   脚本把上次使用的目标 id 记在 STATE_FILE。再次 --real 时，如果对应幂等键已经在
#   entitlement_ledger 里，就跳过调用（不重复扣、也不重复花模型费），并提示
#   「本次未新增消耗」。想重新做一次真实扣量验证时加 --fresh-data。
#
# -----------------------------------------------------------------------------
# 退出码
# -----------------------------------------------------------------------------
#   0  全部通过（SKIP 不算失败）
#   1  有失败项 / 用法错误 / 环境检查失败
#   2  预算超限（立即中止）
#
# 说明：只读 dev 库；不修改 apps/institution、packages/*；不执行 git 操作。
# =============================================================================

set -uo pipefail

# ---------------------------------------------------------------------------
# 0. 基础变量与函数
# ---------------------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
SCRIPT_PATH="$ROOT_DIR/scripts/billing/verify-deduction-e2e.sh"

MODE=""               # check | real | cleanup（空表示未显式指定，最后按 --cleanup 决定）
CHECK_EXPLICIT=0
DO_CLEANUP=0
FRESH_DATA=0
ONLY=""
SHOW_HELP=0

API_BASE="${API_BASE:-http://127.0.0.1:18080}"
API_BASE="${API_BASE%/}"
TENANT_ID="${TENANT_ID:-5001}"
BUDGET_CNY="${BUDGET_CNY:-10}"
HTTP_TIMEOUT="${HTTP_TIMEOUT:-300}"
POLL_TIMEOUT="${POLL_TIMEOUT:-240}"

STATE_FILE="${STATE_FILE:-${XDG_STATE_HOME:-$HOME/.local/state}/lingce-api/verify-deduction-e2e/tenant-${TENANT_ID}.json}"

PRODUCT_ORDER="tingxie yingke hegui suifang kehu huoke"

TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/lingce-deduction-e2e.XXXXXX")"
LOG_FILE="$TMP_DIR/run.log"
: >"$LOG_FILE"
trap 'rm -rf "$TMP_DIR"' EXIT

PASS_COUNT=0
FAIL_COUNT=0
SKIP_COUNT=0
REPLAY_COUNT=0
ROWS=()
FAIL_PRODUCTS=()
SKIP_NOTES=()
WARN_NOTES=()

RUN_START_DB=""
BASE_SPEND="0"
TOTAL_SPEND="0"
CLEANUP_TS=""

# 六个产品的元数据 -----------------------------------------------------------
product_name() {
  case "$1" in
    tingxie) printf '听写病历' ;;
    yingke)  printf '赢客专家' ;;
    hegui)   printf '合规卫士' ;;
    suifang) printf '随访顾问' ;;
    kehu)    printf '客户总监' ;;
    huoke)   printf '获客先锋' ;;
    *)       printf '%s' "$1" ;;
  esac
}

product_key_of_name() {
  case "$1" in
    听写病历) printf 'tingxie' ;;
    赢客专家) printf 'yingke' ;;
    合规卫士) printf 'hegui' ;;
    随访顾问) printf 'suifang' ;;
    客户总监) printf 'kehu' ;;
    获客先锋) printf 'huoke' ;;
    *)        printf '%s' "$1" ;;
  esac
}

product_units() {
  case "$1" in
    tingxie) printf 'record hour' ;;
    yingke)  printf 'hour' ;;
    hegui)   printf 'hour' ;;
    suifang) printf 'hour' ;;
    kehu)    printf 'shot' ;;
    huoke)   printf 'record' ;;
    *)       printf '' ;;
  esac
}

product_path() {
  case "$1" in
    tingxie) printf '/api/v1/internal/emr/records/generate' ;;
    yingke)  printf '/api/v1/internal/deal-expert/recordings/{id}/analyze' ;;
    hegui)   printf '/api/v1/internal/compliance/communication-check' ;;
    suifang) printf '/api/v1/followup/tasks/generate' ;;
    kehu)    printf '/api/v1/internal/patient-operations/screenshots/analyze' ;;
    huoke)   printf '/api/v1/content-items' ;;
    *)       printf '' ;;
  esac
}

product_probe_path() {
  # HEAD 探测用的路径（把 {id} 换成 1；HEAD 命中路由即可，不会真的执行业务）
  product_path "$1" | sed 's/{id}/1/'
}

log()  { printf '%s\n' "$*" | tee -a "$LOG_FILE" >&2; }
say()  { printf '%s\n' "$*" | tee -a "$LOG_FILE"; }
rule() { printf '%s\n' "------------------------------------------------------------------------" | tee -a "$LOG_FILE"; }
die()  { printf 'ERROR: %s\n' "$*" | tee -a "$LOG_FILE" >&2; exit 1; }

usage() {
  awk 'NR==1{next} /^set -uo pipefail/{exit} {print}' "$SCRIPT_PATH" | sed 's/^# \{0,1\}//'
  exit 0
}

parse_args() {
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --check-only) MODE="check"; CHECK_EXPLICIT=1 ;;
      --real)       MODE="real" ;;
      --keep-data)  : ;;                    # 默认行为，显式接受
      --cleanup)    DO_CLEANUP=1 ;;
      --fresh-data) FRESH_DATA=1 ;;
      --only)       shift; [[ $# -gt 0 ]] || die "--only 需要一个参数"; ONLY="${1// /}" ;;
      --only=*)     ONLY="${1#--only=}"; ONLY="${ONLY// /}" ;;
      -h|--help)    SHOW_HELP=1 ;;
      *)            die "未知参数：$1（用 --help 查看用法）" ;;
    esac
    shift
  done

  if [[ "$SHOW_HELP" == "1" ]]; then usage; fi

  if [[ -z "$MODE" ]]; then
    if [[ "$DO_CLEANUP" == "1" ]]; then
      MODE="cleanup"
    else
      MODE="check"
    fi
  fi
  if [[ "$CHECK_EXPLICIT" == "1" && "$DO_CLEANUP" == "1" ]]; then
    die "--cleanup 会写数据，不能与 --check-only 同时使用；请单独执行 --cleanup 或配合 --real"
  fi
  if [[ -n "$ONLY" ]]; then
    local p
    local IFS=','
    for p in $ONLY; do
      case " $PRODUCT_ORDER " in
        *" $p "*) ;;
        *) die "--only 含未知产品：$p（可选：tingxie,yingke,hegui,suifang,kehu,huoke）" ;;
      esac
    done
    unset IFS
  fi
}

selected() {
  [[ -z "$ONLY" ]] && return 0
  case ",$ONLY," in
    *",$1,"*) return 0 ;;
    *) return 1 ;;
  esac
}

is_num() { [[ "$1" =~ ^[0-9]+$ ]]; }
is_uuid() { [[ "$1" =~ ^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$ ]]; }

# ---------------------------------------------------------------------------
# 1. 依赖 / 数据库连接 / 生产库保护
# ---------------------------------------------------------------------------
ensure_tools() {
  command -v curl >/dev/null 2>&1 || die "缺少 curl"
  command -v jq   >/dev/null 2>&1 || die "缺少 jq"
  if ! command -v psql >/dev/null 2>&1; then
    if [[ -x /opt/homebrew/opt/libpq/bin/psql ]]; then
      PATH="/opt/homebrew/opt/libpq/bin:$PATH"
      export PATH
    else
      die "缺少 psql（PostgreSQL 客户端）"
    fi
  fi
}

# 从 configs/dev.toml + secrets 合成 DSN（仅当 DB_DSN 未显式设置时）。
resolve_db_dsn() {
  if [[ -n "${DB_DSN:-}" ]]; then
    return 0
  fi
  local cfg="$ROOT_DIR/configs/dev.toml"
  [[ -f "$cfg" ]] || die "未设置 DB_DSN，且找不到 $cfg；请显式传入 DB_DSN"
  local secrets_rel secrets host port dbname user password
  secrets_rel="$(awk -F' *= *' '/^secrets_file/{gsub(/"/,"",$2); print $2; exit}' "$cfg")"
  secrets="$ROOT_DIR/${secrets_rel#./}"
  host="$(sed -n '/^\[database\]/,/^\[/p' "$cfg"     | awk -F' *= *' '$1=="host"{gsub(/"/,"",$2);print $2;exit}')"
  port="$(sed -n '/^\[database\]/,/^\[/p' "$cfg"     | awk -F' *= *' '$1=="port"{gsub(/"/,"",$2);print $2;exit}')"
  dbname="$(sed -n '/^\[database\]/,/^\[/p' "$cfg"   | awk -F' *= *' '$1=="name"{gsub(/"/,"",$2);print $2;exit}')"
  user="$(sed -n '/^\[database\]/,/^\[/p' "$cfg"     | awk -F' *= *' '$1=="user"{gsub(/"/,"",$2);print $2;exit}')"
  password=""
  if [[ -f "$secrets" ]]; then
    password="$(sed -n '/^\[database\]/,/^\[/p' "$secrets" | awk -F' *= *' '$1=="password"{gsub(/"/,"",$2);print $2;exit}')"
  fi
  [[ -n "$host" && -n "$dbname" && -n "$user" ]] || die "无法从 $cfg 解析数据库配置，请显式传入 DB_DSN"
  if [[ -n "$password" ]]; then
    DB_DSN="postgresql://${user}:${password}@${host}:${port:-5432}/${dbname}?sslmode=disable"
  else
    DB_DSN="postgresql://${user}@${host}:${port:-5432}/${dbname}?sslmode=disable"
  fi
  export DB_DSN
  say "未设置 DB_DSN，已从 configs/dev.toml 合成连接串（数据库：${dbname}@${host}）"
}

mask_dsn() {
  printf '%s' "$1" | sed -E 's#(://[^:/@]+:)[^@]+@#\1****@#'
}

dsn_dbname() { # 从 URL / keyword DSN 里取库名（不解析密码，避免误判）
  local d="$1"
  case "$d" in
    postgresql://*|postgres://*)
      printf '%s' "$d" | sed -E 's#^[a-zA-Z+]+://([^@/]*@)?[^/]+/([^?]+).*#\2#' ;;
    *)
      printf '%s' "$d" | sed -n 's/.*dbname=\([^ ]*\).*/\1/p' ;;
  esac
}

guard_dsn_string() { # 连接前先按库名拦生产库
  local name low
  name="$(dsn_dbname "$DB_DSN")"
  low="$(printf '%s' "$name" | tr '[:upper:]' '[:lower:]')"
  case "$low" in
    *lingce_prod*|*production*|*_prod|prod_*|*-prod|prod-*|*_live|live_*|*prd*)
      printf '\n!! 检测到生产 DSN（库名 %s），直接退出，未建立任何连接。\n' "$name" >&2
      exit 1 ;;
  esac
}

dsn_endpoint() {
  printf '%s' "$1" | sed -E 's#^[a-zA-Z+]+://([^@/]+@)?([^/]+)/([^?]+).*#\2/\3#'
}

is_prod_dbname() {
  local name
  name="$(printf '%s' "$1" | tr '[:upper:]' '[:lower:]')"
  case "$name" in
    lingce_prod|lingce_production|*_prod|prod_*|*-prod|prod-*|*_production|production_*|*prd*|*_live|live_*)
      return 0 ;;
  esac
  return 1
}

db_connect_and_guard() {
  PGCONNECT_TIMEOUT="${PGCONNECT_TIMEOUT:-8}"
  export PGCONNECT_TIMEOUT
  local cur_db
  cur_db="$(psql "$DB_DSN" -X -w -q -A -t -v ON_ERROR_STOP=1 -c 'select current_database()' 2>>"$LOG_FILE" || true)"
  [[ -n "$cur_db" ]] || die "连不上数据库：$(mask_dsn "$DB_DSN")（详见 $LOG_FILE）"

  if is_prod_dbname "$cur_db"; then
    printf '\n' >&2
    printf '!! 检测到生产库：%s\n' "$cur_db" >&2
    printf '!! 本脚本禁止连接生产库，直接退出（未执行任何检查/写入）。\n' >&2
    exit 1
  fi
  case "$(printf '%s' "$cur_db" | tr '[:upper:]' '[:lower:]')" in
    *dev*|*test*|*local*|*stage*) ;;
    *)
      if [[ "${E2E_ALLOW_NON_DEV:-}" != "1" ]]; then
        die "目标库名 '$cur_db' 不像 dev/test 库。确认安全后设置 E2E_ALLOW_NON_DEV=1 再运行。"
      fi
      ;;
  esac
  DB_NAME="$cur_db"
}

# 通用 SQL 执行（只返回标量/单列，多行用 -F 分隔）。不要把大 JSON 走这里。
sql() {
  psql "$DB_DSN" -X -w -q -A -t -v ON_ERROR_STOP=1 -c "$1" 2>>"$LOG_FILE" | tr -d '\r'
}

# 带 psql 变量的 SQL（安全传参，用于幂等键等文本）
sql_v() {
  # 注意：psql 的 -c 不做变量插值，必须从 stdin 读入，:'var' 才会展开。
  local var="$1" val="$2" query="$3"
  printf '%s;\n' "$query" | psql "$DB_DSN" -X -w -q -A -t -v ON_ERROR_STOP=1 -v "$var=$val" 2>>"$LOG_FILE" | tr -d '\r'
}

# 把单行 JSON 结果写到文件（避免超大 payload 走 shell 参数）
sql_to_file() {
  local query="$1" out="$2"
  psql "$DB_DSN" -X -w -q -A -t -v ON_ERROR_STOP=1 -o "$out" -c "$query" 2>>"$LOG_FILE" | tr -d '\r'
}

ledger_has() {
  local key="$1"
  [[ -n "$key" ]] || return 1
  [[ "$(sql_v k "$key" "select exists(select 1 from entitlement_ledger where tenant_id=$TENANT_ID and idempotency_key=:'k')")" == "t" ]]
}

ledger_amount() {
  local key="$1"
  sql_v k "$key" "select coalesce(sum(amount),0)::text from entitlement_ledger where tenant_id=$TENANT_ID and idempotency_key=:'k'"
}

ent_used() {
  local product="$1" unit="$2"
  sql "select coalesce(used,0)::text from product_entitlements
       where tenant_id=$TENANT_ID and product_code='$product' and unit='$unit'
       order by case when quota_type='official' then 0 else 1 end,
                case when edition='standard' then 0 else 1 end, id
       limit 1"
}

spend_since() {
  local since="$1"
  sql_v ts "$since" "select coalesce(sum(total_cost),0)::text from gateway_call_records
                     where tenant_id=$TENANT_ID and created_at >= :'ts'::timestamptz"
}

spend_by_module() {
  sql_v ts "$RUN_START_DB" "select coalesce(caller_module,'unknown')||'|'||count(*)||'|'||coalesce(sum(total_cost),0)::text
                            from gateway_call_records
                            where tenant_id=$TENANT_ID and created_at >= :'ts'::timestamptz
                            group by caller_module
                            order by sum(total_cost) desc"
}

fmt2() { awk -v v="${1:-0}" 'BEGIN{printf "%.2f", v+0}'; }

num_eq() {
  awk -v a="${1:-0}" -v b="${2:-0}" 'BEGIN{d=a-b; if(d<0)d=-d; exit !(d<0.0049)}'
}

hour2() {
  awk -v s="${1:-0}" 'BEGIN{ if (s<=0) { print "0.00"; exit } printf "%.2f", int(s/3600*100+0.5)/100 }'
}

# ---------------------------------------------------------------------------
# 2. 状态文件（幂等目标复用）
# ---------------------------------------------------------------------------
state_init() {
  mkdir -p "$(dirname "$STATE_FILE")" 2>/dev/null || true
  if [[ ! -f "$STATE_FILE" ]]; then
    printf '{"tenant_id":%s,"targets":{}}\n' "$TENANT_ID" >"$STATE_FILE" || die "无法写状态文件：$STATE_FILE"
  fi
}

state_target() { # product -> 紧凑 JSON 或空
  [[ -f "$STATE_FILE" ]] || return 0
  jq -c --arg p "$1" '(.targets[$p] // empty)' "$STATE_FILE" 2>/dev/null
}

state_field() { # product field -> 值或空
  [[ -f "$STATE_FILE" ]] || return 0
  jq -r --arg p "$1" --arg f "$2" '(.targets[$p][$f] // empty) | tostring' "$STATE_FILE" 2>/dev/null
}

state_put() { # product json
  # started_at 只在首次落目标时写入，之后不覆盖：--cleanup 用它圈定「本次运行生成的数据」，
  # 这样「第一次 --real 真扣 + 第二次 --real 全幂等」之后执行清理，仍能清干净第一次的产物。
  local product="$1" payload="$2" tmp
  state_init
  tmp="$(mktemp "${STATE_FILE}.XXXXXX")"
  if jq --arg p "$product" --argjson v "$payload" \
        --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        --arg runstart "${RUN_START_DB:-}" \
        '.targets[$p] = ($v + {
            started_at: (.targets[$p].started_at // (if $runstart == "" then $ts else $runstart end)),
            updated_at: $ts
         }) | .updated_at=$ts' \
        "$STATE_FILE" >"$tmp" 2>>"$LOG_FILE"; then
    mv "$tmp" "$STATE_FILE"
  else
    rm -f "$tmp"
    die "写状态文件失败：$STATE_FILE"
  fi
}

state_set_run_start() {
  local ts="$1" tmp
  state_init
  tmp="$(mktemp "${STATE_FILE}.XXXXXX")"
  if jq --arg ts "$ts" '.last_run_started_at=$ts' "$STATE_FILE" >"$tmp" 2>>"$LOG_FILE"; then
    mv "$tmp" "$STATE_FILE"
  else
    rm -f "$tmp"
  fi
}

state_last_run_start() {
  [[ -f "$STATE_FILE" ]] || return 0
  jq -r '.last_run_started_at // empty' "$STATE_FILE" 2>/dev/null
}

# ---------------------------------------------------------------------------
# 3. 结果表与汇总
# ---------------------------------------------------------------------------
str_dwidth() {
  local s="$1" bytes chars
  bytes=$(printf '%s' "$s" | LC_ALL=C wc -c | tr -d ' ')
  chars=$(printf '%s' "$s" | wc -m | tr -d ' ')
  # 中文/全角字符按 3 字节 2 列估算，ASCII 1 字节 1 列
  printf '%s' $(( chars + (bytes - chars) / 2 ))
}

pad() { # text width
  local s="$1" w="$2" dw n
  dw=$(str_dwidth "$s")
  n=$(( w - dw ))
  [[ $n -lt 0 ]] && n=0
  printf '%s%*s' "$s" "$n" ''
}

# add_row 产品 单位 生成前 生成后 预期 [PASS|FAIL|SKIP] 备注
add_row() {
  local p="$1" u="$2" b="$3" a="$4" e="$5" hint="${6:-}" note="${7:-}"
  local d="-" st status
  if [[ "$hint" == "SKIP" ]]; then
    st="SKIP"; SKIP_COUNT=$((SKIP_COUNT + 1))
  elif [[ "$hint" == "FAIL" ]]; then
    st="❌"; FAIL_COUNT=$((FAIL_COUNT + 1))
    FAIL_PRODUCTS[${#FAIL_PRODUCTS[@]}]="$(product_key_of_name "$p")"
  else
    d=$(awk -v a="$a" -v b="$b" 'BEGIN{printf "%.2f", a-b}')
    if num_eq "$d" "$e"; then
      st="✅"; PASS_COUNT=$((PASS_COUNT + 1))
    else
      st="❌"; FAIL_COUNT=$((FAIL_COUNT + 1))
      FAIL_PRODUCTS[${#FAIL_PRODUCTS[@]}]="$(product_key_of_name "$p")"
    fi
  fi
  ROWS[${#ROWS[@]}]="$p"$'\t'"$u"$'\t'"$b"$'\t'"$a"$'\t'"$d"$'\t'"$e"$'\t'"$st"$'\t'"$note"
}

add_skip() { # product reason
  SKIP_NOTES[${#SKIP_NOTES[@]}]="$1：$2"
  add_row "$(product_name "$1")" "-" "-" "-" "-" "SKIP" "$2"
}

add_warn() { WARN_NOTES[${#WARN_NOTES[@]}]="$1"; }

print_table() {
  local w_p=14 w_u=6 w_n=8 w_s=4
  local hdr sep row p u b a d e st note
  hdr="$(pad '产品' $w_p) | $(pad '单位' $w_u) | $(pad '生成前' $w_n) | $(pad '生成后' $w_n) | $(pad '差额' $w_n) | $(pad '预期' $w_n) | $(pad '结果' $w_s) | 备注"
  sep="$(printf '%*s' 14 '' | tr ' ' '-')-+-$(printf '%*s' 6 '' | tr ' ' '-')-+-$(printf '%*s' 8 '' | tr ' ' '-')-+-$(printf '%*s' 8 '' | tr ' ' '-')-+-$(printf '%*s' 8 '' | tr ' ' '-')-+-$(printf '%*s' 8 '' | tr ' ' '-')-+-$(printf '%*s' 4 '' | tr ' ' '-')-+-----"
  say ""
  rule
  say "逐项对照表（额度单位，用于 生成后-生成前 与 预期 比对）"
  rule
  say "$hdr"
  say "$sep"
  if [[ ${#ROWS[@]} -eq 0 ]]; then
    say "（无结果）"
  else
    for row in "${ROWS[@]}"; do
      IFS=$'\t' read -r p u b a d e st note <<<"$row"
      say "$(pad "$p" $w_p) | $(pad "$u" $w_u) | $(pad "$b" $w_n) | $(pad "$a" $w_n) | $(pad "$d" $w_n) | $(pad "$e" $w_n) | $(pad "$st" $w_s) | $note"
    done
  fi
  rule
}

print_footer() {
  local budget
  budget="$(fmt2 "$BUDGET_CNY")"
  say ""
  rule
  say "总花费 ¥$(fmt2 "$TOTAL_SPEND")（预算 ¥$budget）"
  say "PASS $PASS_COUNT / FAIL $FAIL_COUNT / SKIP $SKIP_COUNT"
  if awk -v s="${TOTAL_SPEND:-0}" 'BEGIN{exit !(s <= 0.0000001)}'; then
    say "本次未新增消耗（全部命中幂等或无需调用模型）"
  fi
  if [[ ${#WARN_NOTES[@]} -gt 0 ]]; then
    local w
    for w in "${WARN_NOTES[@]}"; do say "WARN  $w"; done
  fi
  if [[ ${#SKIP_NOTES[@]} -gt 0 ]]; then
    local s
    for s in "${SKIP_NOTES[@]}"; do say "SKIP  $s"; done
  fi
  if [[ ${#FAIL_PRODUCTS[@]} -gt 0 ]]; then
    say ""
    say "失败项复现命令（--only 只跑对应产品；目标 id 见备注列）："
    local seen="" p
    for p in "${FAIL_PRODUCTS[@]}"; do
      case " $seen " in *" $p "*) continue ;; esac
      seen="$seen $p"
      say "  bash $SCRIPT_PATH --real --only $p"
    done
  fi
  rule
}

# ---------------------------------------------------------------------------
# 4. 预检
# ---------------------------------------------------------------------------
preflight_common() {
  ensure_tools
  is_num "$TENANT_ID" || die "TENANT_ID 必须是正整数：$TENANT_ID"
  resolve_db_dsn
  guard_dsn_string
  state_init
  db_connect_and_guard

  case "$(printf '%s' "$API_BASE" | tr '[:upper:]' '[:lower:]')" in
    http://127.0.0.1*|http://localhost*|http://0.0.0.0*|http://\[::1\]*) ;;
    *)
      if [[ "${E2E_ALLOW_REMOTE_API:-}" != "1" ]]; then
        die "API_BASE 默认只允许本机地址（避免误打线上 API）：$API_BASE。确需远程请设 E2E_ALLOW_REMOTE_API=1"
      fi
      ;;
  esac

  local tenant_ok tables
  tenant_ok="$(sql "select count(*) from tenants where id=$TENANT_ID and deleted_at is null")"
  [[ "$tenant_ok" == "1" ]] || die "租户 $TENANT_ID 不存在或已删除"

  tables="$(sql "select count(*) from information_schema.tables where table_schema='public' and table_name in ('product_entitlements','entitlement_ledger','product_catalog','gateway_call_records')")"
  [[ "$tables" == "4" ]] || die "计费相关表不全（找到 $tables/4）：请确认已跑计费迁移"

  if [[ "$MODE" != "cleanup" ]]; then
    local health
    health="$(curl -sS -m 5 -o /dev/null -w '%{http_code}' "$API_BASE/healthz" 2>/dev/null || true)"
    [[ "$health" == "200" ]] || die "API 不可用：$API_BASE/healthz 返回 ${health:-无响应}（先启动服务）"
  fi

  BUDGET_CNY="$(fmt2 "$BUDGET_CNY")"
  awk -v b="$BUDGET_CNY" 'BEGIN{exit !(b>0)}' || die "BUDGET_CNY 必须是正数：$BUDGET_CNY"

  if [[ -z "${INTERNAL_TOKEN:-}" && -f "$ROOT_DIR/configs/secrets.local.toml" ]]; then
    INTERNAL_TOKEN="$(sed -n '/^\[external\]/,/^\[/p' "$ROOT_DIR/configs/secrets.local.toml" \
      | awk -F' *= *' '$1=="internal_worker_token"{gsub(/"/,"",$2);print $2;exit}')"
    [[ -n "$INTERNAL_TOKEN" ]] && export INTERNAL_TOKEN
  fi
}

resolve_admin_jwt() {
  if [[ -z "${ADMIN_JWT:-}" ]]; then
    return 1
  fi
  if [[ -f "$ADMIN_JWT" ]]; then
    ADMIN_JWT="$(tr -d '\r\n' <"$ADMIN_JWT")"
  fi
  [[ -n "$ADMIN_JWT" ]] || return 1
  return 0
}

preflight_real() {
  [[ "${CONFIRM_SPEND:-}" == "YES" ]] || {
    printf '\n' >&2
    printf '拒绝执行 --real：会真实调用模型并产生费用。\n' >&2
    printf '确认后在命令前加 CONFIRM_SPEND=YES，例如：\n' >&2
    printf '  CONFIRM_SPEND=YES bash %s --real\n\n' "$SCRIPT_PATH" >&2
    exit 1
  }
  [[ -n "${INTERNAL_TOKEN:-}" ]] || die "缺少 INTERNAL_TOKEN（内部接口 token）"
  resolve_admin_jwt || die "缺少 ADMIN_JWT（运营 admin token，或指向 token 文件的路径）；huoke 保存文稿需要 JWT"
}

# ---------------------------------------------------------------------------
# 5. --check-only
# ---------------------------------------------------------------------------
check_pass() { PASS_COUNT=$((PASS_COUNT + 1)); say "  ✅ $*"; }
check_fail() { FAIL_COUNT=$((FAIL_COUNT + 1)); say "  ❌ $*"; }
check_skip() { SKIP_COUNT=$((SKIP_COUNT + 1)); say "  ⏭  $*"; }

run_check_only() {
  local p u rows row

  say ""
  say "=============================================="
  say " 六产品真实扣量验证 · check-only（只读，免费）"
  say "=============================================="
  say "API      : $API_BASE"
  say "租户     : $TENANT_ID"
  say "数据库   : $(dsn_endpoint "$DB_DSN")（库名 $DB_NAME）"
  say "预算     : ¥$(fmt2 "$BUDGET_CNY")（仅 --real 生效）"
  say "状态文件 : $STATE_FILE"
  say "说明     : 本模式只读数据库 + HEAD 探测路由，不调用模型、不写业务数据。"

  # --- 1. 额度行 ---------------------------------------------------------
  say ""
  rule
  say "1. 额度行检查（余额 = granted + rollover_bal - used）"
  rule
  for p in $PRODUCT_ORDER; do
    selected "$p" || continue
    for u in $(product_units "$p"); do
      rows="$(sql "select granted::text||'|'||used::text||'|'||rollover_bal::text||'|'||balance::text||'|'||status||'|'||period_end::text
                   from product_entitlements
                   where tenant_id=$TENANT_ID and product_code='$p' and unit='$u'
                   order by case when quota_type='official' then 0 else 1 end,
                            case when edition='standard' then 0 else 1 end, id
                   limit 1")"
      if [[ -z "$rows" ]]; then
        check_fail "$(product_name "$p") / $u：没有额度行（tenant=$TENANT_ID）"
        continue
      fi
      local granted used rollover balance status period expect_balance
      IFS='|' read -r granted used rollover balance status period <<<"$rows"
      expect_balance=$(awk -v g="$granted" -v r="$rollover" -v u="$used" 'BEGIN{printf "%.2f", g+r-u}')
      local desc="$(product_name "$p") / $u：granted=$granted used=$used rollover=$rollover balance=$balance status=$status 到期=$period"
      if ! num_eq "$balance" "$expect_balance"; then
        check_fail "$desc（余额不等于 granted+rollover-used=$expect_balance）"
      elif [[ "$status" != "active" ]]; then
        check_fail "$desc（状态不是 active）"
      elif [[ "$(sql "select (date '$period' >= current_date)::text")" != "true" ]]; then
        check_fail "$desc（已过期）"
      else
        check_pass "$desc"
      fi
    done
  done

  # --- 2. 流水与幂等键结构 ----------------------------------------------
  say ""
  rule
  say "2. 额度流水与幂等键结构"
  rule
  local n
  n="$(sql "select count(*) from entitlement_ledger where tenant_id=$TENANT_ID and type='usage' and amount >= 0")"
  [[ "$n" == "0" ]] && check_pass "usage 流水金额全部为负（$n 条非负）" || check_fail "有 $n 条 usage 流水金额 >= 0"

  n="$(sql "select count(*) from entitlement_ledger where tenant_id=$TENANT_ID and type='grant' and amount < 0")"
  [[ "$n" == "0" ]] && check_pass "grant 流水金额全部为正（$n 条为负）" || check_fail "有 $n 条 grant 流水金额 < 0"

  n="$(sql "select count(*) from entitlement_ledger where tenant_id=$TENANT_ID and idempotency_key is not null and (length(idempotency_key) > 128 or idempotency_key !~ ':')")"
  [[ "$n" == "0" ]] && check_pass "幂等键长度 <= 128 且含 ':' 分隔（格式 <模块>:<对象>:<动作>）" || check_fail "有 $n 条幂等键格式异常"

  n="$(sql "select count(*) from (select idempotency_key from entitlement_ledger where tenant_id=$TENANT_ID and idempotency_key is not null group by 1 having count(*) > 1) x")"
  [[ "$n" == "0" ]] && check_pass "同一幂等键只出现一次（唯一索引防重）" || check_fail "有 $n 个幂等键重复落账"

  # 组合扣量：同一笔扣量的多单位流水里，只有第一行带 key，其余为 NULL。
  local combo
  combo="$(sql "select count(*) from entitlement_ledger a
                join entitlement_ledger b
                  on b.tenant_id=a.tenant_id and b.product_code=a.product_code
                 and b.reason=a.reason and b.created_at=a.created_at
                 and b.idempotency_key is null
                where a.tenant_id=$TENANT_ID and a.idempotency_key is not null")"
  if [[ "$combo" != "0" ]]; then
    check_pass "组合扣量结构正确：$combo 组「1 条带幂等键 + 其余 NULL」（设计如此，避免唯一索引冲突）"
  else
    check_skip "未找到组合扣量流水（听写病历多单位扣量尚未发生）"
  fi

  # 已知的六产品幂等键前缀
  local known_prefix_count
  known_prefix_count="$(sql "select count(*) from entitlement_ledger
                             where tenant_id=$TENANT_ID and idempotency_key is not null
                               and (idempotency_key like 'emr:record:%:draft'
                                 or idempotency_key like 'deal:recording:%:analysis'
                                 or idempotency_key like 'compliance:run:%:check'
                                 or idempotency_key like 'followup:recording:%:analysis'
                                 or idempotency_key like 'patientops:screenshot:%:review'
                                 or idempotency_key like 'content:article:%:deliver')")"
  check_pass "命中六产品标准幂等键的流水 $known_prefix_count 条"

  # 听写病历组合扣量的实际样本（record 带 key + hour 为 NULL）
  local emr_sample
  emr_sample="$(sql "select count(*) from entitlement_ledger a
                     join entitlement_ledger b
                       on b.tenant_id=a.tenant_id and b.product_code=a.product_code
                      and b.reason=a.reason and b.created_at=a.created_at
                      and b.unit='hour' and b.idempotency_key is null
                     where a.tenant_id=$TENANT_ID and a.product_code='tingxie'
                       and a.unit='record' and a.idempotency_key is not null")"
  if [[ "$emr_sample" != "0" ]]; then
    check_pass "听写病历扣量样本：record 行带 emr:record:*:draft，hour 行幂等键为 NULL"
  else
    check_skip "还没有听写病历「record + hour」组合扣量样本"
  fi

  # 额度流水是否与幂等键一一对应（usage 行数 >= 带 key 行数）
  local usage_rows keyed_rows
  usage_rows="$(sql "select count(*) from entitlement_ledger where tenant_id=$TENANT_ID and type='usage'")"
  keyed_rows="$(sql "select count(*) from entitlement_ledger where tenant_id=$TENANT_ID and type='usage' and idempotency_key is not null")"
  say "  ℹ️  usage 流水 $usage_rows 条，其中带幂等键 $keyed_rows 条（差额为同笔扣量的其他单位行）"

  # --- 3. 数据可用性 -----------------------------------------------------
  say ""
  rule
  say "3. 目标数据可用性（--real 会从这里挑数据）"
  rule
  local recordings written doctor_recs doctors_with_dept donors topics empty_runs state_lines
  recordings="$(sql "select count(*) from recordings where tenant_id=$TENANT_ID and deleted_at is null")"
  written="$(sql "select count(*) from recordings where tenant_id=$TENANT_ID and deleted_at is null
                    and cleaned_transcription is not null and cleaned_transcription::text like '[%'")"
  doctor_recs="$(sql "select count(*) from recordings r
                      join employees e on e.id=r.employee_id and e.tenant_id=r.tenant_id and e.deleted_at is null
                      join institution_employee_roles er on er.tenant_id=e.tenant_id and er.employee_id=e.id and lower(trim(er.role_code))='doctor'
                      where r.tenant_id=$TENANT_ID and r.deleted_at is null
                        and r.cleaned_transcription::text like '[%' and r.duration>0
                        and coalesce(e.medical_department_code,'')<>'' and r.encounter_id is not null")"
  donors="$(sql "select count(*) from patient_conversation_sources where source_type='screenshot'
                   and media_data like 'data:image/%' and analysis_status='succeeded'")"
  topics="$(sql "select count(*) from content_topics where tenant_id=$TENANT_ID")"
  empty_runs="$(sql "select count(*) from followup_generation_runs where tenant_id=$TENANT_ID and last_error_code='EMPTY_DELIVERY'")"

  say "  录音总数（未删除）        : $recordings"
  say "  有转写且清理完成的录音    : $written"
  say "  可跑听写病历的医生录音    : $doctor_recs"
  say "  截图源（任一租户，可用）  : $donors"
  say "  本租户选题                : $topics"
  say "  随访空交付历史 run        : $empty_runs"

  [[ "$recordings" != "0" ]]  && check_pass "目标租户有 $recordings 条录音" || check_fail "目标租户没有录音"
  [[ "$written" != "0" ]]     && check_pass "有 $written 条带转写的录音可用于赢客/合规/随访" || check_fail "没有带转写的录音"
  [[ "$doctor_recs" != "0" ]] && check_pass "有 $doctor_recs 条医生录音可用于听写病历" || check_skip "没有医生录音，听写病历将 SKIP"
  [[ "$donors" != "0" ]]      && check_pass "有 $donors 张已分析截图可复用为截图源" || check_skip "没有可用截图，客户总监将 SKIP"
  if [[ "$topics" != "0" ]]; then
    check_pass "本租户有 $topics 个选题，获客先锋用真实选题"
  else
    check_skip "本租户没有选题（--real 时会自动新建一条回归专用选题）"
  fi

  # 随访空交付不扣量：历史证据
  local empty_key_check empty_sql
  empty_sql="select r.id, r.recording_id, r.persisted_task_count,
                    exists(select 1 from entitlement_ledger l
                            where l.tenant_id=r.tenant_id
                              and l.idempotency_key='followup:recording:'||r.recording_id||':analysis')::text
             from followup_generation_runs r
             where r.tenant_id=$TENANT_ID and r.last_error_code='EMPTY_DELIVERY'
               and not exists (select 1 from followup_generation_runs s
                               where s.tenant_id=r.tenant_id and s.recording_id=r.recording_id
                                 and s.status='succeeded')
             order by r.id desc limit 1"
  empty_key_check="$(sql "$empty_sql")"
  if [[ -n "$empty_key_check" ]]; then
    local run_id rec_id task_count charged
    IFS='|' read -r run_id rec_id task_count charged <<<"$empty_key_check"
    if [[ "$charged" == "false" ]]; then
      check_pass "随访空交付不扣量：run#$run_id（recording#$rec_id, tasks=$task_count）没有对应额度流水"
    else
      check_fail "随访空交付却扣了额度：run#$run_id（recording#$rec_id）已有 followup:recording:*:analysis 流水"
    fi
  else
    check_skip "尚无随访空交付历史 run，无法用历史数据验证「0 条任务不扣」"
  fi

  # --- 4. 六个生成接口可达（HEAD） --------------------------------------
  say ""
  rule
  say "4. 六个产品生成接口可达性（HEAD，不执行、不调模型）"
  rule
  for p in $PRODUCT_ORDER; do
    selected "$p" || continue
    local code path
    path="$(product_probe_path "$p")"
    code="$(curl -s -I -m 8 -o /dev/null -w '%{http_code}' "$API_BASE$path" 2>/dev/null || true)"
    case "$code" in
      200|204|401|403|405)
        check_pass "$(product_name "$p")：HEAD $path -> $code（路由存在）" ;;
      404)
        check_fail "$(product_name "$p")：HEAD $path -> 404（路由不存在）" ;;
      000|"")
        check_fail "$(product_name "$p")：HEAD $path -> 无响应" ;;
      *)
        check_fail "$(product_name "$p")：HEAD $path -> $code" ;;
    esac
  done

  # --- 5. 上次运行目标与幂等状态 ----------------------------------------
  say ""
  rule
  say "5. 上次 --real 目标与幂等状态（$STATE_FILE）"
  rule
  if [[ -f "$STATE_FILE" ]]; then
    say "  last_run_started_at: $(state_last_run_start)"
    for p in $PRODUCT_ORDER; do
      selected "$p" || continue
      local t key rid
      t="$(state_target "$p")"
      if [[ -z "$t" || "$t" == "null" ]]; then
        say "  - $(product_name "$p")：尚未记录验证目标（下次 --real 会现挑数据）"
        continue
      fi
      key="$(printf '%s' "$t" | jq -r '.key // empty')"
      rid="$(printf '%s' "$t" | jq -r '.recording_id // .source_id // .article_id // .record_id // empty')"
      if [[ -n "$key" ]] && ledger_has "$key"; then
        say "  - $(product_name "$p")：目标 $rid，幂等键已扣过 -> 重复 --real 会跳过调用（不产生新费用）"
        say "       $key"
      else
        say "  - $(product_name "$p")：目标 $rid，幂等键未扣过 -> 下次 --real 会真实扣量"
        [[ -n "$key" ]] && say "       $key"
      fi
    done
  else
    say "  状态文件不存在；首次 --real 会现场挑选未扣过的数据。"
  fi

  say ""
  rule
  say "check-only 完成：未调用任何模型接口，未写业务数据，花费 ¥0.00"
  say "PASS $PASS_COUNT / FAIL $FAIL_COUNT / SKIP $SKIP_COUNT"
  if [[ ${#WARN_NOTES[@]} -gt 0 ]]; then
    local w
    for w in "${WARN_NOTES[@]}"; do say "WARN  $w"; done
  fi
  rule

  [[ "$FAIL_COUNT" -eq 0 ]] && exit 0 || exit 1
}

# ---------------------------------------------------------------------------
# 6. 目标数据挑选（每类都优先挑「幂等键未落账」的数据，保证首次 --real 真扣量）
# ---------------------------------------------------------------------------
pick_tingxie_target() {
  sql "select json_build_object(
             'recording_id', r.id,
             'encounter_id', r.encounter_id,
             'employee_id', r.employee_id,
             'duration', r.duration,
             'record_id', coalesce(er.id::text,''),
             'key', case when er.id is null then '' else 'emr:record:'||er.id::text||':draft' end
           )::text
       from recordings r
       join employees e on e.id=r.employee_id and e.tenant_id=r.tenant_id and e.deleted_at is null
       join institution_employee_roles ier on ier.tenant_id=e.tenant_id and ier.employee_id=e.id
                                        and lower(trim(ier.role_code))='doctor'
       left join emr_records er on er.tenant_id=r.tenant_id and er.encounter_id=r.encounter_id
       where r.tenant_id=$TENANT_ID and r.deleted_at is null
         and r.cleaned_transcription is not null and r.cleaned_transcription::text like '[%'
         and r.duration between 60 and 3600
         and r.encounter_id is not null and r.encounter_id > 0
         and coalesce(e.medical_department_code,'') <> ''
         and (er.id is null or not exists (
               select 1 from entitlement_ledger l
               where l.tenant_id=r.tenant_id
                 and l.idempotency_key='emr:record:'||er.id::text||':draft'))
       order by r.duration desc, r.id desc
       limit 1"
}

pick_yingke_target() {
  sql "select json_build_object(
             'recording_id', r.id,
             'duration', r.duration,
             'key', 'deal:recording:'||r.id||':analysis'
           )::text
       from recordings r
       where r.tenant_id=$TENANT_ID and r.deleted_at is null
         and r.cleaned_transcription is not null and r.cleaned_transcription::text like '[%'
         and r.duration between 300 and 3600
         and not exists (
           select 1 from entitlement_ledger l
           where l.tenant_id=r.tenant_id
             and l.idempotency_key='deal:recording:'||r.id||':analysis')
       order by r.duration desc, r.id desc
       limit 1"
}

pick_hegui_target() {
  local exclude="${1:-0}"
  sql "select json_build_object(
             'recording_id', r.id,
             'duration', r.duration,
             'key', ''
           )::text
       from recordings r
       join employees e on e.id=r.employee_id and e.tenant_id=r.tenant_id and e.deleted_at is null
       left join institution_employee_roles ier on ier.tenant_id=e.tenant_id and ier.employee_id=e.id
       where r.tenant_id=$TENANT_ID and r.deleted_at is null
         and r.cleaned_transcription is not null and r.cleaned_transcription::text like '[%'
         and r.duration between 120 and 900
         and r.customer_id is not null and r.encounter_id is not null and r.employee_id is not null
         and r.recorded_at is not null
         and r.id <> $exclude
         and coalesce(e.name,'') <> ''
         and coalesce(ier.role_code,'') <> 'sales'
         and not exists (
           select 1 from compliance_check_runs cr
           where cr.tenant_id=r.tenant_id and cr.source_id=r.id::text
             and cr.status in ('completed','processing'))
       order by r.duration desc, r.id desc
       limit 1"
}

pick_suifang_target() {
  local exclude="${1:-0}"
  # 与 followup.Service.validateReferences 完全同构：随访只面向医生录音，
  # 且接诊记录必须能由录音派生（recording）或由实时医生录音绑定（realtime）。
  sql "select json_build_object(
             'recording_id', r.id,
             'duration', r.duration,
             'encounter_id', r.encounter_id,
             'employee_id', r.employee_id,
             'customer_id', r.customer_id,
             'key', 'followup:recording:'||r.id||':analysis'
           )::text
       from recordings r
       join encounters en
         on en.id=r.encounter_id and en.tenant_id=r.tenant_id
        and ((en.source_type='recording' and en.source_id=r.id)
             or (en.source_type='realtime' and en.provider_id=r.employee_id))
       join employees e
         on e.id=r.employee_id and e.tenant_id=r.tenant_id and e.deleted_at is null
        and (en.source_type <> 'realtime' or (
              select lower(trim(er.role_code)) from institution_employee_roles er
              where er.tenant_id=e.tenant_id and er.employee_id=e.id
              order by er.updated_at desc nulls last, er.created_at desc, er.id desc limit 1
            ) = 'doctor')
       join customers c
         on c.id=r.customer_id and c.tenant_id=r.tenant_id and c.deleted_at is null
        and (en.source_type <> 'realtime' or en.patient_id is null or en.patient_id=c.id)
       join tenants t on t.id=r.tenant_id and t.deleted_at is null
       where r.tenant_id=$TENANT_ID and r.deleted_at is null
         and r.cleaned_transcription is not null and r.cleaned_transcription::text like '[%'
         and r.duration between 300 and 3600
         and r.customer_id is not null and r.encounter_id is not null and r.employee_id is not null
         and r.recorded_at is not null
         and r.id <> $exclude
         and coalesce(e.name,'') <> '' and coalesce(c.name,'') <> ''
         and (lower(trim(coalesce(r.business_scope,'')))='doctor'
              or lower(trim(coalesce(r.resolved_role_code,''))) in ('doctor','doctor_patient','doctor_conversion'))
         and not exists (
           select 1 from entitlement_ledger l
           where l.tenant_id=r.tenant_id
             and l.idempotency_key='followup:recording:'||r.id||':analysis')
         and not exists (
           select 1 from followup_generation_runs f
           where f.tenant_id=r.tenant_id and f.recording_id=r.id
             and f.last_error_code='EMPTY_DELIVERY')
       order by r.duration desc, r.id desc
       limit 1"
}

pick_kehu_target() {
  # 优先挑一张「本租户还没分析过」的截图（content_hash 未出现），这样首次调用会真扣 shot 1。
  sql "select json_build_object('image_source_id', s.id, 'media_len', length(s.media_data))::text
       from patient_conversation_sources s
       where s.source_type='screenshot'
         and s.media_data like 'data:image/%'
         and coalesce(s.analysis_status,'')='succeeded'
         and s.tenant_id <> $TENANT_ID
         and length(s.media_data) < 400000
         and not exists (
           select 1 from patient_conversation_sources x
           where x.tenant_id=$TENANT_ID and x.content_hash=s.content_hash)
       order by s.id desc
       limit 1"
}

pick_kehu_donor_fallback() {
  sql "select json_build_object('image_source_id', s.id, 'media_len', length(s.media_data))::text
       from patient_conversation_sources s
       where s.source_type='screenshot'
         and s.media_data like 'data:image/%'
         and coalesce(s.analysis_status,'')='succeeded'
         and length(s.media_data) < 400000
       order by s.id desc
       limit 1"
}

pick_huoke_topic() {
  sql "select id from content_topics where tenant_id=$TENANT_ID order by id desc limit 1"
}

pick_huoke_existing_article() {
  local topic="$1"
  [[ "$topic" != "0" ]] || return 0
  sql "select id from content_items
       where tenant_id=$TENANT_ID and topic_id=$topic and deleted_at is null
       order by id desc limit 1"
}

# 构造六个产品的请求体（写到文件，避免 transcript/image 走 shell 参数）
build_emr_payload() {
  local rid="$1" out="$2"
  sql_to_file "select json_build_object(
      'tenant_id', r.tenant_id,
      'recording_id', r.id,
      'encounter_id', r.encounter_id,
      'employee_id', r.employee_id,
      'customer_id', coalesce(r.customer_id,0),
      'customer_name', coalesce(c.name,''),
      'doctor_name', coalesce(e.name,''),
      'business_scope', 'doctor',
      'role_code', 'doctor',
      'recorded_at', case when r.recorded_at is null then ''
                          else to_char(r.recorded_at,'YYYY-MM-DD\"T\"HH24:MI:SS')||'+08:00' end,
      'cleaned_transcript', coalesce((
        select string_agg(x->>'text', E'\n' order by ord)
        from jsonb_array_elements(r.cleaned_transcription) with ordinality t(x, ord)),'')
    )::text
    from recordings r
    join employees e on e.id=r.employee_id and e.tenant_id=r.tenant_id
    left join customers c on c.id=r.customer_id and c.tenant_id=r.tenant_id
    where r.id=$rid and r.tenant_id=$TENANT_ID" "$out"
}

build_common_payload() { # recording_id out [extra]
  local rid="$1" out="$2"
  sql_to_file "select json_build_object(
      'tenant_id', r.tenant_id,
      'recording_id', r.id,
      'encounter_id', r.encounter_id,
      'employee_id', r.employee_id,
      'customer_id', coalesce(r.customer_id,0),
      'customer_name', coalesce(c.name,''),
      'employee_name', coalesce(e.name,''),
      'doctor_name', coalesce(e.name,''),
      'business_scope', coalesce(nullif(lower(trim(ier.role_code)),''),'consultant'),
      'role_code', coalesce(nullif(lower(trim(ier.role_code)),''),'consultant'),
      'recorded_at', to_char(r.recorded_at,'YYYY-MM-DD\"T\"HH24:MI:SS')||'+08:00',
      'cleaned_transcript', coalesce((
        select string_agg(x->>'text', E'\n' order by ord)
        from jsonb_array_elements(r.cleaned_transcription) with ordinality t(x, ord)),'')
    )::text
    from recordings r
    join employees e on e.id=r.employee_id and e.tenant_id=r.tenant_id
    left join customers c on c.id=r.customer_id and c.tenant_id=r.tenant_id
    left join lateral (
      select role_code from institution_employee_roles z
      where z.tenant_id=r.tenant_id and z.employee_id=r.employee_id
      order by z.updated_at desc nulls last, z.id desc limit 1
    ) ier on true
    where r.id=$rid and r.tenant_id=$TENANT_ID" "$out"
}

# ---------------------------------------------------------------------------
# 7. HTTP / 轮询
# ---------------------------------------------------------------------------
http_post() { # url auth_header body_file out_file -> code
  local url="$1" auth="$2" body="$3" out="$4"
  curl -sS -m "$HTTP_TIMEOUT" -o "$out" -w '%{http_code}' \
    -X POST "$url" -H 'Content-Type: application/json' -H "$auth" \
    --data-binary "@$body" 2>>"$LOG_FILE" || true
}

wait_for_key() { # key timeout
  local key="$1" timeout="${2:-20}" elapsed=0
  while [[ $elapsed -lt $timeout ]]; do
    ledger_has "$key" && return 0
    sleep 1
    elapsed=$((elapsed + 1))
  done
  return 1
}

poll_value() { # sql interval timeout -> 一整行（第一列是状态；非 pending/processing 即返回）
  local query="$1" interval="${2:-4}" timeout="${3:-$POLL_TIMEOUT}" elapsed=0 val="" head
  while :; do
    val="$(sql "$query")"
    head="${val%%|*}"
    case "$head" in
      ""|"pending"|"processing"|"accepted"|"running"|"retry_wait") ;;
      *) printf '%s' "$val"; return 0 ;;
    esac
    if [[ $elapsed -ge $timeout ]]; then
      printf '%s' "$val"
      return 1
    fi
    sleep "$interval"
    elapsed=$((elapsed + interval))
  done
}

# ---------------------------------------------------------------------------
# 8. 预算控制
# ---------------------------------------------------------------------------
current_spend() { spend_since "$RUN_START_DB"; }

budget_guard() {
  local phase="$1" spend
  spend="$(current_spend)"
  if awk -v s="$spend" -v b="$BUDGET_CNY" 'BEGIN{exit !(s>b)}'; then
    say ""
    say "!! 预算超限，立即中止（$phase）"
    say "!! 已花费 ¥$(fmt2 "$spend") > 预算 ¥$(fmt2 "$BUDGET_CNY")"
    TOTAL_SPEND="$spend"
    print_table
    print_footer
    exit 2
  fi
}

# ---------------------------------------------------------------------------
# 9. 六个产品的真实调用
# ---------------------------------------------------------------------------
run_tingxie() {
  local name target rid dur rec_id key before_r before_h after_r after_h code exp_hour
  name="$(product_name tingxie)"
  target="$(state_target tingxie)"

  if [[ -n "$target" && "$target" != "null" ]]; then
    rec_id="$(printf '%s' "$target" | jq -r '.record_id // empty')"
    key="$(printf '%s' "$target" | jq -r '.key // empty')"
    if [[ -n "$key" ]] && ledger_has "$key"; then
      rid="$(printf '%s' "$target" | jq -r '.recording_id')"
      before_r="$(ent_used tingxie record)"; before_h="$(ent_used tingxie hour)"
      add_row "$name" "record" "$before_r" "$before_r" "0" "PASS" \
        "幂等命中（$key），跳过调用；录音#$rid 病历#${rec_id:0:8}"
      add_row "$name" "hour" "$before_h" "$before_h" "0" "PASS" \
        "幂等命中，未重复扣 hour；录音#$rid"
      REPLAY_COUNT=$((REPLAY_COUNT + 1))
      say "  ↺ $name：幂等键已扣过，跳过调用（录音#$rid）"
      return 0
    fi
    rid="$(printf '%s' "$target" | jq -r '.recording_id')"
  else
    target="$(pick_tingxie_target)"
    if [[ -z "$target" ]]; then
      add_skip tingxie "找不到「医生有科室 + 有转写 + 未扣过」的录音"
      say "  ⏭  $name：找不到可用录音，SKIP"
      return 0
    fi
    state_put tingxie "$target"
    rid="$(printf '%s' "$target" | jq -r '.recording_id')"
  fi

  dur="$(sql "select coalesce(duration,0) from recordings where id=$rid and tenant_id=$TENANT_ID")"
  exp_hour="$(hour2 "$dur")"
  build_emr_payload "$rid" "$TMP_DIR/emr.json"
  if [[ ! -s "$TMP_DIR/emr.json" || "$(head -c 1 "$TMP_DIR/emr.json")" != "{" ]]; then
    add_row "$name" "record" "-" "-" "1.00" "FAIL" "构造请求体失败：录音#$rid 不存在或缺转写"
    return 0
  fi

  say "  → $name：录音#$rid 时长 ${dur}s，预期 record 1 + hour $exp_hour"
  before_r="$(ent_used tingxie record)"; before_h="$(ent_used tingxie hour)"
  code="$(http_post "$API_BASE/api/v1/internal/emr/records/generate" \
          "X-Internal-Token: $INTERNAL_TOKEN" "$TMP_DIR/emr.json" "$TMP_DIR/emr.resp")"
  rec_id="$(jq -r '.record_id // empty' "$TMP_DIR/emr.resp" 2>/dev/null || true)"
  key=""
  [[ -n "$rec_id" ]] && key="emr:record:${rec_id}:draft"
  if [[ -n "$key" ]]; then wait_for_key "$key" 15 || true; fi
  after_r="$(ent_used tingxie record)"; after_h="$(ent_used tingxie hour)"

  if [[ "$code" != "200" ]]; then
    local msg
    msg="$(jq -r '.message // .error // empty' "$TMP_DIR/emr.resp" 2>/dev/null || true)"
    add_row "$name" "record" "-" "-" "1.00" "FAIL" "HTTP $code ${msg:-（无 message）}；录音#$rid"
    add_row "$name" "hour" "-" "-" "$exp_hour" "FAIL" "HTTP $code；录音#$rid"
    say "  ❌ $name：HTTP $code（$(head -c 300 "$TMP_DIR/emr.resp" | tr '\n' ' ')）"
    return 0
  fi

  state_put tingxie "$(jq -nc --argjson t "$target" --argjson rid "$rid" --arg rec "$rec_id" --arg key "$key" \
    '{recording_id:$rid, encounter_id:$t.encounter_id, employee_id:$t.employee_id, duration:$t.duration, record_id:$rec, key:$key}')"

  add_row "$name" "record" "$before_r" "$after_r" "1.00" "PASS" "病历#$rec_id 录音#$rid"
  add_row "$name" "hour" "$before_h" "$after_h" "$exp_hour" "PASS" "录音 ${dur}s → ${exp_hour}h"
  say "  ✅ $name：record $before_r→$after_r，hour $before_h→$after_h（病历#$rec_id）"
}

run_yingke() {
  local name target rid dur key before after code status err
  name="$(product_name yingke)"
  target="$(state_target yingke)"
  if [[ -n "$target" && "$target" != "null" ]]; then
    rid="$(printf '%s' "$target" | jq -r '.recording_id')"
    key="$(printf '%s' "$target" | jq -r '.key // empty')"
    [[ -n "$key" ]] || key="deal:recording:${rid}:analysis"
    if ledger_has "$key"; then
      before="$(ent_used yingke hour)"
      add_row "$name" "hour" "$before" "$before" "0" "PASS" "幂等命中（$key），跳过调用；录音#$rid"
      REPLAY_COUNT=$((REPLAY_COUNT + 1))
      say "  ↺ $name：幂等键已扣过，跳过调用（录音#$rid）"
      return 0
    fi
    dur="$(printf '%s' "$target" | jq -r '.duration // 0')"
  else
    target="$(pick_yingke_target)"
    if [[ -z "$target" ]]; then
      add_skip yingke "找不到「有转写 + 300~3600s + 未扣过」的录音"
      say "  ⏭  $name：找不到可用录音，SKIP"
      return 0
    fi
    state_put yingke "$target"
    rid="$(printf '%s' "$target" | jq -r '.recording_id')"
    dur="$(printf '%s' "$target" | jq -r '.duration // 0')"
  fi
  key="deal:recording:${rid}:analysis"
  local exp_hour attempt=1 max_attempts=2 poll
  exp_hour="$(hour2 "$dur")"
  printf '{"tenant_id":%s,"recording_id":%s}\n' "$TENANT_ID" "$rid" >"$TMP_DIR/yingke.json"

  while [[ $attempt -le $max_attempts ]]; do
    before="$(ent_used yingke hour)"
    say "  → $name：录音#$rid 时长 ${dur}s，预期 hour $exp_hour（异步分析，第 $attempt 次）"
    code="$(http_post "$API_BASE/api/v1/internal/deal-expert/recordings/$rid/analyze" \
            "X-Internal-Token: $INTERNAL_TOKEN" "$TMP_DIR/yingke.json" "$TMP_DIR/yingke.resp")"
    if [[ "$code" != "200" && "$code" != "202" ]]; then
      say "  ⚠️  $name：第 $attempt 次 HTTP $code"
    else
      poll="$(poll_value "select status||'|'||coalesce(error_message,'') from consultation_analyses where tenant_id=$TENANT_ID and recording_id=$rid" 4 "$POLL_TIMEOUT" || true)"
      IFS='|' read -r status err <<<"${poll:-failed|poll_timeout}"
      if [[ "$status" == "analyzed" ]]; then
        wait_for_key "$key" 15 || true
        after="$(ent_used yingke hour)"
        state_put yingke "$(jq -nc --argjson rid "$rid" --argjson dur "$dur" --arg key "$key" \
          '{recording_id:$rid, duration:$dur, key:$key}')"
        add_row "$name" "hour" "$before" "$after" "$exp_hour" "PASS" "成交分析完成；录音#$rid"
        say "  ✅ $name：hour $before→$after（录音#$rid）"
        return 0
      fi
      say "  ⚠️  $name：第 $attempt 次分析未成功（status=$status ${err:+：$err}）"
    fi
    if [[ $attempt -lt $max_attempts ]]; then
      say "  ↻ $name：重试一次（模型网关偶发取消/超时）"
      attempt=$((attempt + 1))
      sleep 2
      continue
    fi
    add_row "$name" "hour" "-" "-" "$exp_hour" "FAIL" "分析未成功（status=$status ${err:+：$err}）；录音#$rid"
    say "  ❌ $name：分析未成功（status=$status ${err}）"
    return 0
  done
}

run_hegui() {
  local name target rid dur key run_id before after code exp_hour attempt=1 max_attempts=2
  name="$(product_name hegui)"
  target="$(state_target hegui)"
  if [[ -n "$target" && "$target" != "null" ]]; then
    rid="$(printf '%s' "$target" | jq -r '.recording_id')"
    run_id="$(printf '%s' "$target" | jq -r '.run_id // empty')"
    key="$(printf '%s' "$target" | jq -r '.key // empty')"
    if [[ -n "$key" ]] && ledger_has "$key"; then
      before="$(ent_used hegui hour)"
      add_row "$name" "hour" "$before" "$before" "0" "PASS" "幂等命中（$key），跳过调用；录音#$rid"
      REPLAY_COUNT=$((REPLAY_COUNT + 1))
      say "  ↺ $name：幂等键已扣过，跳过调用（录音#$rid）"
      return 0
    fi
    dur="$(printf '%s' "$target" | jq -r '.duration // 0')"
  else
    target="$(pick_hegui_target)"
    if [[ -z "$target" ]]; then
      add_skip hegui "找不到「有转写 + 客户/员工/接诊齐全 + 未检查过」的录音"
      say "  ⏭  $name：找不到可用录音，SKIP"
      return 0
    fi
    state_put hegui "$target"
    rid="$(printf '%s' "$target" | jq -r '.recording_id')"
    dur="$(printf '%s' "$target" | jq -r '.duration // 0')"
  fi

  while [[ $attempt -le $max_attempts ]]; do
    exp_hour="$(hour2 "$dur")"
    before="$(ent_used hegui hour)"
    build_common_payload "$rid" "$TMP_DIR/hegui.json"
    say "  → $name：录音#$rid 时长 ${dur}s，预期 hour $exp_hour（同步检查，第 $attempt 次）"
    code="$(http_post "$API_BASE/api/v1/internal/compliance/communication-check" \
            "X-Internal-Token: $INTERNAL_TOKEN" "$TMP_DIR/hegui.json" "$TMP_DIR/hegui.resp")"
    if [[ "$code" == "200" ]]; then
      run_id="$(jq -r '.run_id // empty' "$TMP_DIR/hegui.resp")"
      if [[ -z "$run_id" ]]; then
        add_row "$name" "hour" "$before" "$(ent_used hegui hour)" "$exp_hour" "FAIL" "接口未返回 run_id，无法核对幂等键；录音#$rid"
        return 0
      fi
      key="compliance:run:${run_id}:check"
      wait_for_key "$key" 20 || true
      after="$(ent_used hegui hour)"
      state_put hegui "$(jq -nc --argjson rid "$rid" --argjson dur "$dur" --arg run "$run_id" --arg key "$key" \
        '{recording_id:$rid, duration:$dur, run_id:$run, key:$key}')"
      add_row "$name" "hour" "$before" "$after" "$exp_hour" "PASS" "检查 run#$run_id；录音#$rid"
      say "  ✅ $name：hour $before→$after（run#$run_id）"
      return 0
    fi

    local msg
    msg="$(jq -r '.message // .error // empty' "$TMP_DIR/hegui.resp" 2>/dev/null || true)"
    say "  ⚠️  $name：第 $attempt 次 HTTP $code（${msg:-无 message}）"
    # 失败可能来自模型输出不满足契约（未落库、未扣量）。换一条更短的录音重试一次。
    if [[ $attempt -lt $max_attempts ]]; then
      target="$(pick_hegui_target "$rid")"
      if [[ -n "$target" ]]; then
        rid="$(printf '%s' "$target" | jq -r '.recording_id')"
        dur="$(printf '%s' "$target" | jq -r '.duration // 0')"
        state_put hegui "$target"
        attempt=$((attempt + 1))
        continue
      fi
    fi
    add_row "$name" "hour" "-" "-" "$exp_hour" "FAIL" "HTTP $code ${msg:+：$msg}；录音#$rid"
    say "  ❌ $name：HTTP $code（$(head -c 300 "$TMP_DIR/hegui.resp" | tr '\n' ' ')）"
    return 0
  done
}

run_suifang() {
  local name target rid dur key before after code exp_hour
  name="$(product_name suifang)"
  target="$(state_target suifang)"
  if [[ -n "$target" && "$target" != "null" ]]; then
    rid="$(printf '%s' "$target" | jq -r '.recording_id')"
    key="$(printf '%s' "$target" | jq -r '.key // empty')"
    [[ -n "$key" ]] || key="followup:recording:${rid}:analysis"
    if ledger_has "$key"; then
      before="$(ent_used suifang hour)"
      add_row "$name" "hour" "$before" "$before" "0" "PASS" "幂等命中（$key），跳过调用；录音#$rid"
      REPLAY_COUNT=$((REPLAY_COUNT + 1))
      say "  ↺ $name：幂等键已扣过，跳过调用（录音#$rid）"
      return 0
    fi
    dur="$(printf '%s' "$target" | jq -r '.duration // 0')"
  else
    target="$(pick_suifang_target)"
    if [[ -z "$target" ]]; then
      add_skip suifang "找不到「有转写 + 客户/员工/接诊齐全 + 未扣过」的录音"
      say "  ⏭  $name：找不到可用录音，SKIP"
      return 0
    fi
    state_put suifang "$target"
    rid="$(printf '%s' "$target" | jq -r '.recording_id')"
    dur="$(printf '%s' "$target" | jq -r '.duration // 0')"
  fi
  exp_hour="$(hour2 "$dur")"
  before="$(ent_used suifang hour)"

  local attempt=1 max_attempts=3 run_label poll status tasks err run_db_id
  while [[ $attempt -le $max_attempts ]]; do
    run_label="billing-e2e-${TENANT_ID}-$(date +%s)-${attempt}"
    build_common_payload "$rid" "$TMP_DIR/suifang.json"
    # analysis_run_id 必须是 RFC3339 之外的稳定业务标识；这里补进请求体
    if ! jq --arg r "$run_label" '. + {analysis_run_id:$r}' "$TMP_DIR/suifang.json" >"$TMP_DIR/suifang.req" 2>>"$LOG_FILE"; then
      add_row "$name" "hour" "-" "-" "$exp_hour" "FAIL" "构造请求体失败；录音#$rid"
      return 0
    fi
    say "  → $name：录音#$rid 时长 ${dur}s，预期 hour $exp_hour（异步，第 $attempt 次，run=$run_label）"
    code="$(http_post "$API_BASE/api/v1/followup/tasks/generate" \
            "X-Internal-Token: $INTERNAL_TOKEN" "$TMP_DIR/suifang.req" "$TMP_DIR/suifang.resp")"
    if [[ "$code" != "200" ]]; then
      add_row "$name" "hour" "-" "-" "$exp_hour" "FAIL" "HTTP $code；录音#$rid"
      say "  ❌ $name：HTTP $code（$(head -c 300 "$TMP_DIR/suifang.resp" | tr '\n' ' ')）"
      return 0
    fi
    poll="$(poll_value "select status||'|'||coalesce(persisted_task_count,0)||'|'||coalesce(last_error_code,'')||'|'||id||'|'||coalesce(left(replace(last_error_message,chr(10),' '),160),'')
                        from followup_generation_runs
                        where tenant_id=$TENANT_ID and analysis_run_id='$run_label'" 4 "$POLL_TIMEOUT" || true)"
    IFS='|' read -r status tasks err run_db_id _msg <<<"${poll:-failed|0|POLL_TIMEOUT|0|}"

    if [[ "$status" == "succeeded" && "$tasks" != "0" ]]; then
      key="followup:recording:${rid}:analysis"
      wait_for_key "$key" 15 || true
      after="$(ent_used suifang hour)"
      state_put suifang "$(jq -nc --argjson rid "$rid" --argjson dur "$dur" --arg key "$key" \
        --arg run "$run_label" --argjson ridb "${run_db_id:-0}" \
        '{recording_id:$rid, duration:$dur, key:$key, analysis_run_id:$run, run_db_id:$ridb}')"
      add_row "$name" "hour" "$before" "$after" "$exp_hour" "PASS" "随访任务 ${tasks} 条；录音#$rid"
      say "  ✅ $name：hour $before→$after（tasks=$tasks）"
      return 0
    fi

    if [[ "$status" == "failed" ]]; then
      if [[ "$err" == "EMPTY_DELIVERY" ]]; then
        say "  ⚠️  $name：第 $attempt 次空交付（0 条任务，按规则不扣量），换一条录音重试"
      else
        say "  ⚠️  $name：第 $attempt 次生成失败（$err${_msg:+：$_msg}），换一条录音重试"
      fi
      # 空交付是合法业务结果、其他失败是模型/校验问题，都不是本脚本的 FAIL；
      # 在重试次数内换一条未扣过的录音再验一次。
      if [[ $attempt -lt $max_attempts ]]; then
        target="$(pick_suifang_target "$rid")"
        if [[ -n "$target" ]]; then
          rid="$(printf '%s' "$target" | jq -r '.recording_id')"
          dur="$(printf '%s' "$target" | jq -r '.duration // 0')"
          exp_hour="$(hour2 "$dur")"
          state_put suifang "$target"
          attempt=$((attempt + 1))
          before="$(ent_used suifang hour)"
          continue
        fi
      fi
      if [[ "$err" == "EMPTY_DELIVERY" ]]; then
        before="$(ent_used suifang hour)"
        add_row "$name" "hour" "$before" "$before" "0" "PASS" "连续空交付，按规则未扣量（0 条任务不扣）"
        say "  ⏭  $name：连续空交付，按规则不扣量（业务规则已验证）"
      else
        add_row "$name" "hour" "-" "-" "$exp_hour" "FAIL" "生成失败 $err${_msg:+：$_msg}；录音#$rid"
        say "  ❌ $name：生成失败（$err${_msg:+：$_msg}）"
      fi
      return 0
    fi

    add_row "$name" "hour" "-" "-" "$exp_hour" "FAIL" "轮询超时：状态=$status；录音#$rid"
    say "  ❌ $name：轮询超时（status=$status）"
    return 0
  done
}

run_kehu() {
  local name target image_source before after code reused source_id employee_id
  name="$(product_name kehu)"
  target="$(state_target kehu)"

  if [[ -n "$target" && "$target" != "null" ]]; then
    source_id="$(printf '%s' "$target" | jq -r '.source_id // empty')"
    local key
    key="$(printf '%s' "$target" | jq -r '.key // empty')"
    [[ -n "$key" ]] || { [[ -n "$source_id" ]] && key="patientops:screenshot:${source_id}:review"; }
    if [[ -n "$key" ]] && ledger_has "$key"; then
      before="$(ent_used kehu shot)"
      add_row "$name" "shot" "$before" "$before" "0" "PASS" "幂等命中（$key），跳过调用；截图源#$source_id"
      REPLAY_COUNT=$((REPLAY_COUNT + 1))
      say "  ↺ $name：幂等键已扣过，跳过调用（source#$source_id）"
      return 0
    fi
    image_source="$(printf '%s' "$target" | jq -r '.image_source_id // empty')"
  fi

  if [[ -z "${image_source:-}" || "$image_source" == "null" ]]; then
    target="$(pick_kehu_target)"
    if [[ -z "$target" ]]; then
      target="$(pick_kehu_donor_fallback)"
      [[ -n "$target" ]] && say "  ℹ️  $name：没有「本租户未分析过」的截图，改用已分析截图（会命中服务端 content_hash 复用，不扣量）"
    fi
    if [[ -z "$target" ]]; then
      add_skip kehu "patient_conversation_sources 里没有可用截图"
      say "  ⏭  $name：没有可用截图，SKIP"
      return 0
    fi
    image_source="$(printf '%s' "$target" | jq -r '.image_source_id')"
  fi

  employee_id="$(sql "select id from employees
                      where tenant_id=$TENANT_ID and deleted_at is null and coalesce(is_active,1)=1
                      order by id limit 1")"
  if ! is_num "$employee_id"; then
    add_skip kehu "目标租户没有可用员工，无法提报截图分析"
    say "  ⏭  $name：没有可用员工，SKIP"
    return 0
  fi

  # 组装请求体（image_data 直接从库里取，避免超大参数）
  sql_to_file "select json_build_object(
      'tenant_id', $TENANT_ID,
      'employee_id', $employee_id,
      'original_name', 'billing-e2e.png',
      'image_data', s.media_data
    )::text
    from patient_conversation_sources s
    where s.id=$image_source and s.media_data like 'data:image/%'" "$TMP_DIR/kehu.json"
  if [[ ! -s "$TMP_DIR/kehu.json" || "$(head -c 1 "$TMP_DIR/kehu.json")" != "{" ]]; then
    add_skip kehu "截图源#$image_source 不存在或 media_data 不是 data:image/*"
    say "  ⏭  $name：截图源不可用，SKIP"
    return 0
  fi

  before="$(ent_used kehu shot)"
  say "  → $name：复用截图源#$image_source 做一次截图分析，预期 shot 1"
  code="$(http_post "$API_BASE/api/v1/internal/patient-operations/screenshots/analyze" \
          "X-Internal-Token: $INTERNAL_TOKEN" "$TMP_DIR/kehu.json" "$TMP_DIR/kehu.resp")"
  if [[ "$code" != "200" ]]; then
    local msg
    msg="$(jq -r '.message // .error // empty' "$TMP_DIR/kehu.resp" 2>/dev/null || true)"
    add_row "$name" "shot" "-" "-" "1.00" "FAIL" "HTTP $code ${msg:+：$msg}；截图源#$image_source"
    say "  ❌ $name：HTTP $code（$(head -c 300 "$TMP_DIR/kehu.resp" | tr '\n' ' ')）"
    return 0
  fi
  source_id="$(jq -r '.source_id // empty' "$TMP_DIR/kehu.resp")"
  reused="$(jq -r '.reused // false' "$TMP_DIR/kehu.resp")"
  wait_for_key "patientops:screenshot:${source_id}:review" 10 || true
  after="$(ent_used kehu shot)"

  if [[ "$reused" == "true" ]]; then
    state_put kehu "$(jq -nc --argjson img "$image_source" --argjson sid "$source_id" \
      '{image_source_id:$img, source_id:$sid, reused:true, key:("patientops:screenshot:"+($sid|tostring)+":review")}')"
    add_row "$name" "shot" "$before" "$after" "0" "PASS" "服务端 content_hash 复用既有分析（source#$source_id），不重复扣"
    say "  ↺ $name：复用既有分析，未扣量（source#$source_id）"
    return 0
  fi

  state_put kehu "$(jq -nc --argjson img "$image_source" --argjson sid "$source_id" \
    '{image_source_id:$img, source_id:$sid, reused:false, key:("patientops:screenshot:"+($sid|tostring)+":review")}')"
  add_row "$name" "shot" "$before" "$after" "1.00" "PASS" "截图分析完成；source#$source_id"
  say "  ✅ $name：shot $before→$after（source#$source_id）"
}

run_huoke() {
  local name topic article key before after code body_title topic_created=0
  name="$(product_name huoke)"
  topic="$(state_field huoke topic_id)"
  article="$(state_field huoke article_id)"
  key=""

  if [[ -n "$article" && "$article" != "null" ]]; then
    key="content:article:${article}:deliver"
    if ledger_has "$key"; then
      before="$(ent_used huoke record)"
      add_row "$name" "record" "$before" "$before" "0" "PASS" "幂等命中（$key），跳过调用；文稿#$article"
      REPLAY_COUNT=$((REPLAY_COUNT + 1))
      say "  ↺ $name：幂等键已扣过，跳过调用（文稿#$article）"
      return 0
    fi
  fi

  if [[ -z "$topic" || "$topic" == "null" ]]; then
    topic="$(pick_huoke_topic)"
    if [[ -z "$topic" ]]; then
      # 本租户没有选题：新建一条回归专用选题（不会调用模型）
      printf '{"title":"[计费回归] 文稿保存扣量验证","description":"billing e2e","form":"graphic","goal":"verify"}\n' >"$TMP_DIR/topic.json"
      code="$(http_post "$API_BASE/api/v1/content-topics" "Authorization: Bearer $ADMIN_JWT" "$TMP_DIR/topic.json" "$TMP_DIR/topic.resp")"
      if [[ "$code" != "200" ]]; then
        add_skip huoke "本租户没有选题，且自动新建选题失败（HTTP $code）"
        say "  ⏭  $name：无选题且新建失败，SKIP"
        return 0
      fi
      topic="$(jq -r '.id // empty' "$TMP_DIR/topic.resp")"
      topic_created=1
      # 先落状态：即使后续保存文稿失败，--cleanup 也能删掉这条测试选题
      state_put huoke "$(jq -nc --argjson t "$topic" '{topic_id:$t, topic_created:true, article_id:0, key:""}')"
      say "  ℹ️  $name：本租户没有选题，已新建回归专用选题#$topic（--cleanup 会一并清理）"
    fi
  fi

  # 已有同选题文稿时直接复用（服务端「同一选题只保留一条内容」，article id 稳定 → 幂等）
  local existing
  existing="$(pick_huoke_existing_article "$topic")"
  if [[ -n "$existing" ]]; then
    key="content:article:${existing}:deliver"
    if ledger_has "$key"; then
      before="$(ent_used huoke record)"
      state_put huoke "$(jq -nc --argjson t "$topic" --argjson a "$existing" --argjson tc "$topic_created" --arg key "$key" \
        '{topic_id:$t, topic_created:($tc==1), article_id:$a, key:$key}')"
      add_row "$name" "record" "$before" "$before" "0" "PASS" "幂等命中（$key），跳过调用；选题#$topic 文稿#$existing"
      REPLAY_COUNT=$((REPLAY_COUNT + 1))
      say "  ↺ $name：同选题文稿已扣过，跳过调用（文稿#$existing）"
      return 0
    fi
    article="$existing"
  fi

  body_title="[计费回归] 文稿保存扣量 $(date +%Y%m%d-%H%M%S)"
  jq -nc --argjson topic "$topic" --arg title "$body_title" \
    '{topic_id:$topic, form:"graphic", title:$title, body:"计费回归脚本自动生成，用于验证文稿保存扣 record 1。", publish_package:{}, images:{}}' \
    >"$TMP_DIR/huoke.json"

  before="$(ent_used huoke record)"
  say "  → $name：保存文稿（选题#$topic），预期 record 1"
  code="$(http_post "$API_BASE/api/v1/content-items" "Authorization: Bearer $ADMIN_JWT" "$TMP_DIR/huoke.json" "$TMP_DIR/huoke.resp")"
  if [[ "$code" != "200" ]]; then
    local msg
    msg="$(jq -r '.message // .error // empty' "$TMP_DIR/huoke.resp" 2>/dev/null || true)"
    state_put huoke "$(jq -nc --argjson t "$topic" --argjson tc "$topic_created" \
      '{topic_id:$t, topic_created:($tc==1), article_id:0, key:""}')"
    add_row "$name" "record" "-" "-" "1.00" "FAIL" "HTTP $code ${msg:+：$msg}；选题#$topic"
    say "  ❌ $name：HTTP $code（$(head -c 300 "$TMP_DIR/huoke.resp" | tr '\n' ' ')）"
    return 0
  fi
  article="$(jq -r '.id // empty' "$TMP_DIR/huoke.resp")"
  key="content:article:${article}:deliver"
  wait_for_key "$key" 10 || true
  after="$(ent_used huoke record)"
  state_put huoke "$(jq -nc --argjson t "$topic" --argjson a "$article" --argjson tc "$topic_created" --arg key "$key" \
    '{topic_id:$t, topic_created:($tc==1), article_id:$a, key:$key}')"
  add_row "$name" "record" "$before" "$after" "1.00" "PASS" "文稿#$article 保存成功（选题#$topic）"
  say "  ✅ $name：record $before→$after（文稿#$article）"
}

# ---------------------------------------------------------------------------
# 10. 清理（只删业务产物，不动额度流水）
# ---------------------------------------------------------------------------
cleanup_sql_count() { # delete-sql（可引用 :'ts'）-> 删除行数
  sql_v ts "$CLEANUP_TS" "with d as ($1 returning 1) select count(*) from d"
}

run_cleanup() {
  local ts target rec_id run_id rid article topic auto_cust
  ts="$(state_last_run_start)"
  [[ -n "$ts" ]] || ts="1970-01-01 00:00:00+00"
  CLEANUP_TS="$ts"
  cleanup_ts_for() { # product -> 该产品目标首次落库时间
    local t
    t="$(state_target "$1")"
    if [[ -n "$t" && "$t" != "null" ]]; then
      t="$(printf '%s' "$t" | jq -r '.started_at // empty')"
      [[ -n "$t" ]] && { printf '%s' "$t"; return; }
    fi
    printf '%s' "$ts"
  }

  say ""
  rule
  say "清理本次/上次 --real 生成的业务数据（额度流水保持不动）"
  say "运行起点：$ts"
  rule

  local ledger_before ledger_after
  ledger_before="$(sql "select count(*) from entitlement_ledger where tenant_id=$TENANT_ID")"

  CLEANUP_TS="$(cleanup_ts_for tingxie)"
  # 听写病历：emr_records（子表 emr_record_snapshots / emr_quality_runs / emr_record_score_current 级联）
  target="$(state_target tingxie)"
  if [[ -n "$target" && "$target" != "null" ]]; then
    rec_id="$(printf '%s' "$target" | jq -r '.record_id // empty')"
    if is_uuid "$rec_id"; then
      local n1 n2
      n1="$(cleanup_sql_count "delete from emr_process_records where tenant_id=$TENANT_ID and record_id='$rec_id'")"
      n2="$(cleanup_sql_count "delete from emr_records where tenant_id=$TENANT_ID and id='$rec_id'")"
      say "  听写病历：删除 process_records $n1 行，病历 $n2 行（#$rec_id）"
    else
      say "  听写病历：状态里没有有效病历 id，跳过"
    fi
  fi

  CLEANUP_TS="$(cleanup_ts_for yingke)"
  # 赢客专家：consultation_analyses + 由分析重建的 sales_tasks
  target="$(state_target yingke)"
  if [[ -n "$target" && "$target" != "null" ]]; then
    rid="$(printf '%s' "$target" | jq -r '.recording_id // empty')"
    if is_num "$rid"; then
      local n1 n2
      n1="$(cleanup_sql_count "delete from sales_tasks where tenant_id=$TENANT_ID and recording_id=$rid and created_at >= :'ts'::timestamptz")"
      n2="$(cleanup_sql_count "delete from consultation_analyses where tenant_id=$TENANT_ID and recording_id=$rid and updated_at >= :'ts'::timestamptz")"
      say "  赢客专家：删除成交分析 $n2 行，销售任务 $n1 行（录音#$rid）"
    fi
  fi

  CLEANUP_TS="$(cleanup_ts_for hegui)"
  # 合规卫士：run（findings/item_sources 级联）+ 本次生成的 check_items
  target="$(state_target hegui)"
  if [[ -n "$target" && "$target" != "null" ]]; then
    run_id="$(printf '%s' "$target" | jq -r '.run_id // empty')"
    rid="$(printf '%s' "$target" | jq -r '.recording_id // empty')"
    if is_uuid "$run_id"; then
      local n1 n2
      n1="$(cleanup_sql_count "delete from compliance_check_items where tenant_id=$TENANT_ID and source_type='recording' and source_id='$rid' and created_at >= :'ts'::timestamptz")"
      n2="$(cleanup_sql_count "delete from compliance_check_runs where tenant_id=$TENANT_ID and id='$run_id'::uuid")"
      say "  合规卫士：删除检查 run $n2 行、检查项 $n1 行（run#$run_id）"
    fi
  fi

  CLEANUP_TS="$(cleanup_ts_for suifang)"
  # 随访顾问：recording_tasks（source_type=follow_up）+ generation run。空交付重试会产生多个 run，
  # 统一按脚本前缀 billing-e2e- 删除本次运行产生的 run，任务按这些 run 的录音删除。
  target="$(state_target suifang)"
  if [[ -n "$target" && "$target" != "null" ]]; then
    local n1 n2
    n1="$(cleanup_sql_count "delete from recording_tasks
          where tenant_id=$TENANT_ID and source_type in ('follow_up','followup') and created_at >= :'ts'::timestamptz
            and recording_id in (
              select recording_id from followup_generation_runs
              where tenant_id=$TENANT_ID and analysis_run_id like 'billing-e2e-%'
                and created_at >= :'ts'::timestamptz)")"
    n2="$(cleanup_sql_count "delete from followup_generation_runs
          where tenant_id=$TENANT_ID and analysis_run_id like 'billing-e2e-%'
            and created_at >= :'ts'::timestamptz")"
    say "  随访顾问：删除随访任务 $n1 行、生成 run $n2 行（含空交付重试）"
  fi

  CLEANUP_TS="$(cleanup_ts_for kehu)"
  # 客户总监：截图源 + 自动创建待确认客户
  target="$(state_target kehu)"
  if [[ -n "$target" && "$target" != "null" ]]; then
    local sid
    sid="$(printf '%s' "$target" | jq -r '.source_id // empty')"
    if is_num "$sid"; then
      auto_cust="$(sql "select coalesce(auto_created_customer_id,0) from patient_conversation_sources where tenant_id=$TENANT_ID and id=$sid")"
      local n1 n2 n3 n4 n5 n6
      n1="$(cleanup_sql_count "delete from patient_operation_records where tenant_id=$TENANT_ID and source_id=$sid")"
      n2="$(cleanup_sql_count "delete from patient_operation_task_drafts where tenant_id=$TENANT_ID and source_id=$sid")"
      n3="$(cleanup_sql_count "delete from patient_operation_tasks where tenant_id=$TENANT_ID and source_id=$sid")"
      n4="$(cleanup_sql_count "delete from screenshot_customer_reviews where tenant_id=$TENANT_ID and source_id=$sid")"
      n5="$(cleanup_sql_count "delete from patient_conversation_sources where tenant_id=$TENANT_ID and id=$sid")"
      n6=0
      if is_num "$auto_cust" && [[ "$auto_cust" != "0" ]]; then
        n6="$(cleanup_sql_count "delete from customers where tenant_id=$TENANT_ID and id=$auto_cust
              and created_at >= :'ts'::timestamptz
              and extra_data->>'screenshot_auto_lead'='true'
              and extra_data->>'identity_status'='pending_confirm'")"
        [[ "$n6" != "0" ]] && cleanup_sql_count "delete from customer_identities where customer_id=$auto_cust" >/dev/null
      fi
      say "  客户总监：删除截图源 $n5 行（关联记录 ${n1}/${n2}/${n3}/${n4} 行），自动线索客户 $n6 行（source#$sid）"
    fi
  fi

  CLEANUP_TS="$(cleanup_ts_for huoke)"
  # 获客先锋：文稿 + 回归专用选题（仅当没有其他文稿时）
  target="$(state_target huoke)"
  if [[ -n "$target" && "$target" != "null" ]]; then
    article="$(printf '%s' "$target" | jq -r '.article_id // empty')"
    topic="$(printf '%s' "$target" | jq -r '.topic_id // empty')"
    local n1 n2
    n1=0; n2=0
    if is_num "$article"; then
      n1="$(cleanup_sql_count "delete from content_items where tenant_id=$TENANT_ID and id=$article")"
    fi
    if is_num "$topic"; then
      n2="$(cleanup_sql_count "delete from content_topics ct where ct.tenant_id=$TENANT_ID and ct.id=$topic
            and ct.title like '[计费回归]%'
            and not exists (select 1 from content_items ci where ci.topic_id=ct.id)")"
    fi
    say "  获客先锋：删除文稿 $n1 行、回归专用选题 $n2 行"
  fi

  ledger_after="$(sql "select count(*) from entitlement_ledger where tenant_id=$TENANT_ID")"
  if [[ "$ledger_before" == "$ledger_after" ]]; then
    say "  额度流水未改动：$ledger_before 条（清理前）→ $ledger_after 条（清理后）"
  else
    say "  ⚠️  额度流水条数变化：$ledger_before → $ledger_after（请人工核查）"
    add_warn "cleanup 后额度流水条数变化：$ledger_before → $ledger_after"
  fi
  rule
}

# ---------------------------------------------------------------------------
# 11. --real 主流程
# ---------------------------------------------------------------------------
run_real() {
  preflight_real

  say ""
  say "=============================================="
  say " 六产品真实扣量验证 · real（会花钱）"
  say "=============================================="
  say "API      : $API_BASE"
  say "租户     : $TENANT_ID"
  say "数据库   : $(dsn_endpoint "$DB_DSN")（库名 $DB_NAME）"
  say "预算     : ¥$(fmt2 "$BUDGET_CNY")"
  say "状态文件 : $STATE_FILE"
  [[ "$FRESH_DATA" == "1" ]] && say "数据策略 : --fresh-data（忽略旧目标，重新挑未扣过的数据）"
  say ""

  RUN_START_DB="$(sql 'select now()::text')"
  state_set_run_start "$RUN_START_DB"
  BASE_SPEND="$(current_spend)"
  say "运行起点 : $RUN_START_DB"
  say "基线花费 : ¥$(fmt2 "$BASE_SPEND")（gateway_call_records 窗口汇总）"
  say ""

  if [[ "$FRESH_DATA" == "1" ]]; then
    # 只清掉本次要跑的产品目标（--only 时不影响其他产品的清理记录），重新挑数据
    local p tmp
    tmp="$(mktemp "${STATE_FILE}.XXXXXX")"
    if jq --arg list "$ONLY" --arg all "$PRODUCT_ORDER" '
        (.targets // {}) as $t
        | if ($list == "") then .targets = {}
          else .targets = ($t | with_entries(. as $e | select(($list | split(",")) | index($e.key) | not)))
          end' "$STATE_FILE" >"$tmp"; then mv "$tmp" "$STATE_FILE"; fi
  fi

  local p
  for p in $PRODUCT_ORDER; do
    selected "$p" || continue
    budget_guard "调用 $(product_name "$p") 之前"
    case "$p" in
      tingxie) run_tingxie ;;
      yingke)  run_yingke ;;
      hegui)   run_hegui ;;
      suifang) run_suifang ;;
      kehu)    run_kehu ;;
      huoke)   run_huoke ;;
    esac
    budget_guard "完成 $(product_name "$p") 之后"
  done

  sleep 1
  TOTAL_SPEND="$(current_spend)"

  print_table
  rule
  say "花费明细（本次窗口内按调用模块汇总，含失败调用）"
  rule
  local module calls cost
  while IFS='|' read -r module calls cost; do
    [[ -n "$module" ]] || continue
    say "  $(pad "$module" 40) $(pad "$calls 次" 8) ¥$(awk -v c="$cost" 'BEGIN{printf "%.4f", c+0}')"
  done < <(spend_by_module 2>/dev/null || true)
  print_footer

  if [[ "$DO_CLEANUP" == "1" ]]; then
    run_cleanup
  else
    say "数据已保留（默认 --keep-data）：跑完不删，业务方可逐个产品查看；需要清理加 --cleanup。"
  fi

  if [[ "$FAIL_COUNT" -gt 0 ]]; then
    exit 1
  fi
  exit 0
}

# ---------------------------------------------------------------------------
# 12. main
# ---------------------------------------------------------------------------
main() {
  parse_args "$@"
  preflight_common

  case "$MODE" in
    check)   run_check_only ;;
    cleanup) run_cleanup; exit 0 ;;
    real)    run_real ;;
    *)       die "未知模式：$MODE" ;;
  esac
}

main "$@"

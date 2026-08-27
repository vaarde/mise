#!/usr/bin/env bash
#
# seed-sandbox.sh — populate a Square SANDBOX account with a catalog that
# exercises every path in Mise's Square adapter.
#
# Creates: 2 taxes, 2 categories, 2 discounts (percentage + fixed amount),
# 1 modifier list with 2 modifiers, and 2 items — one fully wired up with a
# category, taxes, a modifier list and two priced variations, one minimal.
#
# Everything goes up in a single batch-upsert. Square resolves the "#temp"
# IDs objects use to reference each other within one batch, so nothing has
# to be created first and looked up afterwards.
#
# Usage:
#   export SQUARE_ACCESS_TOKEN='EAAAl...'      # SANDBOX token
#   ./scripts/seed-sandbox.sh                  # create the catalog
#   ./scripts/seed-sandbox.sh --verify         # list what is there now
#   ./scripts/seed-sandbox.sh --clean          # DELETE the whole catalog
#
# Re-running the seed is safe: the idempotency key is fixed, so Square
# returns the original result instead of creating duplicates.

set -euo pipefail

readonly SANDBOX_HOST="https://connect.squareupsandbox.com"
readonly HOST="${SQUARE_API_HOST:-$SANDBOX_HOST}"
readonly API_VERSION="2024-01-18"
readonly IDEMPOTENCY_KEY="mise-seed-v1"

readonly BOLD=$'\033[1m'
readonly DIM=$'\033[2m'
readonly RED=$'\033[31m'
readonly GREEN=$'\033[32m'
readonly RESET=$'\033[0m'

die() {
	printf '%s\n' "${RED}Error:${RESET} $*" >&2
	exit 1
}

require_token() {
	if [[ -z "${SQUARE_ACCESS_TOKEN:-}" ]]; then
		die "SQUARE_ACCESS_TOKEN is not set.

Copy your SANDBOX access token from developer.squareup.com/apps
(switch the environment toggle to Sandbox, then open Credentials), then:

  export SQUARE_ACCESS_TOKEN='EAAAl...'"
	fi
}

# square METHOD PATH [BODY_FILE] — call the Square API, print the response,
# and fail loudly on an HTTP error.
square() {
	local method="$1" path="$2" body_file="${3:-}"
	local response status

	local -a args=(
		--silent
		--show-error
		--write-out $'\n%{http_code}'
		--request "$method"
		--header "Authorization: Bearer ${SQUARE_ACCESS_TOKEN}"
		--header "Square-Version: ${API_VERSION}"
		--header "Content-Type: application/json"
	)
	[[ -n "$body_file" ]] && args+=(--data "@${body_file}")

	response="$(curl "${args[@]}" "${HOST}${path}")"
	status="${response##*$'\n'}"
	response="${response%$'\n'*}"

	if [[ "$status" -ge 400 ]]; then
		printf '%s\n' "$response" >&2
		die "Square returned HTTP ${status} for ${method} ${path}

A 401 almost always means the token is for the wrong environment: this
script talks to the sandbox, so a production token will be rejected."
	fi

	printf '%s' "$response"
}

# summarize PATH — print "TYPE  count" for the objects at a catalog path.
count_objects() {
	local square_type="$1" response count
	response="$(square GET "/v2/catalog/list?types=${square_type}")"
	count="$(printf '%s' "$response" | grep -o "\"type\": *\"${square_type}\"" | wc -l | tr -d ' ')"
	printf '  %-16s %s\n' "$square_type" "$count"
}

verify() {
	printf '%s\n' "${BOLD}Catalog in the sandbox now:${RESET}"
	for square_type in TAX CATEGORY ITEM DISCOUNT MODIFIER_LIST; do
		count_objects "$square_type"
	done
}

clean() {
	# Refuse to wipe anything but the sandbox, whatever SQUARE_API_HOST says.
	if [[ "$HOST" != "$SANDBOX_HOST" && "$HOST" != http://127.0.0.1:* ]]; then
		die "--clean only runs against the sandbox, not ${HOST}"
	fi

	printf '%s\n' "${RED}${BOLD}This deletes EVERY catalog object in the sandbox account.${RESET}"
	printf '%s\n' "${DIM}Host: ${HOST}${RESET}"
	printf 'Type %s to continue: ' "${BOLD}yes${RESET}"

	local answer
	read -r answer
	[[ "$answer" == "yes" ]] || die "Cancelled — nothing was deleted."

	local ids
	ids="$(square GET "/v2/catalog/list?types=ITEM,CATEGORY,TAX,DISCOUNT,MODIFIER_LIST" |
		grep -o '"id": *"[A-Z0-9]\{10,\}"' |
		sed 's/.*"\([A-Z0-9]\{10,\}\)"/\1/' |
		sort -u |
		sed 's/^/"/; s/$/"/' |
		paste -sd, -)"

	if [[ -z "$ids" ]]; then
		printf '%s\n' "Catalog is already empty."
		return
	fi

	local payload
	payload="$(mktemp)"
	printf '{"object_ids":[%s]}' "$ids" >"$payload"

	square POST "/v2/catalog/batch-delete" "$payload" >/dev/null
	rm -f "$payload"

	printf '%s\n' "${GREEN}Catalog deleted.${RESET}"
}

seed() {
	local payload
	payload="$(mktemp)"

	# Quoted heredoc delimiter: the shell does not touch a single character
	# of this JSON, so there is no escaping to get wrong.
	cat >"$payload" <<'JSON'
{
  "idempotency_key": "mise-seed-v1",
  "batches": [
    {
      "objects": [
        {
          "type": "TAX",
          "id": "#ga_state_sales_tax",
          "present_at_all_locations": true,
          "tax_data": {
            "name": "GA State Sales Tax",
            "calculation_phase": "TAX_SUBTOTAL_PHASE",
            "inclusion_type": "ADDITIVE",
            "percentage": "4.5",
            "applies_to_custom_amounts": true,
            "enabled": true
          }
        },
        {
          "type": "TAX",
          "id": "#ga_alcohol_tax",
          "present_at_all_locations": true,
          "tax_data": {
            "name": "GA Alcohol Tax",
            "calculation_phase": "TAX_SUBTOTAL_PHASE",
            "inclusion_type": "ADDITIVE",
            "percentage": "6.0",
            "applies_to_custom_amounts": false,
            "enabled": true
          }
        },
        {
          "type": "CATEGORY",
          "id": "#beverages",
          "present_at_all_locations": true,
          "category_data": { "name": "Beverages" }
        },
        {
          "type": "CATEGORY",
          "id": "#food",
          "present_at_all_locations": true,
          "category_data": { "name": "Food" }
        },
        {
          "type": "DISCOUNT",
          "id": "#happy_hour",
          "present_at_all_locations": true,
          "discount_data": {
            "name": "Happy Hour",
            "discount_type": "FIXED_PERCENTAGE",
            "percentage": "15",
            "pin_required": false,
            "label_color": "9da2a6"
          }
        },
        {
          "type": "DISCOUNT",
          "id": "#five_dollars_off",
          "present_at_all_locations": true,
          "discount_data": {
            "name": "Five Dollars Off",
            "discount_type": "FIXED_AMOUNT",
            "amount_money": { "amount": 500, "currency": "USD" },
            "pin_required": true
          }
        },
        {
          "type": "MODIFIER_LIST",
          "id": "#size",
          "present_at_all_locations": true,
          "modifier_list_data": {
            "name": "Size",
            "selection_type": "SINGLE",
            "modifiers": [
              {
                "type": "MODIFIER",
                "id": "#size_small",
                "present_at_all_locations": true,
                "modifier_data": {
                  "name": "Small",
                  "price_money": { "amount": 0, "currency": "USD" }
                }
              },
              {
                "type": "MODIFIER",
                "id": "#size_large",
                "present_at_all_locations": true,
                "modifier_data": {
                  "name": "Large",
                  "price_money": { "amount": 100, "currency": "USD" }
                }
              }
            ]
          }
        },
        {
          "type": "ITEM",
          "id": "#summer_lemonade",
          "present_at_all_locations": true,
          "item_data": {
            "name": "Summer Lemonade",
            "description": "Fresh-squeezed lemonade with mint",
            "abbreviation": "LEM",
            "product_type": "REGULAR",
            "category_id": "#beverages",
            "tax_ids": ["#ga_state_sales_tax"],
            "modifier_list_info": [
              { "modifier_list_id": "#size", "enabled": true }
            ],
            "variations": [
              {
                "type": "ITEM_VARIATION",
                "id": "#summer_lemonade_regular",
                "present_at_all_locations": true,
                "item_variation_data": {
                  "item_id": "#summer_lemonade",
                  "name": "Regular",
                  "pricing_type": "FIXED_PRICING",
                  "price_money": { "amount": 450, "currency": "USD" }
                }
              },
              {
                "type": "ITEM_VARIATION",
                "id": "#summer_lemonade_large",
                "present_at_all_locations": true,
                "item_variation_data": {
                  "item_id": "#summer_lemonade",
                  "name": "Large",
                  "pricing_type": "FIXED_PRICING",
                  "price_money": { "amount": 595, "currency": "USD" }
                }
              }
            ]
          }
        },
        {
          "type": "ITEM",
          "id": "#house_burger",
          "present_at_all_locations": true,
          "item_data": {
            "name": "House Burger",
            "product_type": "REGULAR",
            "category_id": "#food",
            "tax_ids": ["#ga_state_sales_tax", "#ga_alcohol_tax"],
            "variations": [
              {
                "type": "ITEM_VARIATION",
                "id": "#house_burger_single",
                "present_at_all_locations": true,
                "item_variation_data": {
                  "item_id": "#house_burger",
                  "name": "Single",
                  "pricing_type": "FIXED_PRICING",
                  "price_money": { "amount": 1250, "currency": "USD" }
                }
              }
            ]
          }
        }
      ]
    }
  ]
}
JSON

	printf '%s\n' "${BOLD}Seeding ${HOST}${RESET}"
	square POST "/v2/catalog/batch-upsert" "$payload" >/dev/null
	rm -f "$payload"
	printf '%s\n\n' "${GREEN}Done.${RESET}"

	verify

	cat <<EOF

${BOLD}Next:${RESET}
  cd /c/temp/mise-test
  mise fetch
EOF
}

main() {
	require_token

	case "${1:-seed}" in
	seed) seed ;;
	--verify | verify) verify ;;
	--clean | clean) clean ;;
	*) die "Unknown argument: $1 (expected --verify or --clean)" ;;
	esac
}

main "$@"

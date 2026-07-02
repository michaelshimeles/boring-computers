#!/usr/bin/env bash
#
# deploy-web.sh - Deploy apps/web to production.
#
# Just pushes main. Vercel's git integration builds the SvelteKit app (Root
# Directory = apps/web) and auto-promotes boringcomputers.com + www to the new
# production deployment — no manual aliasing needed (the domains are configured
# as the project's production domains, so they follow the latest prod deploy).
#
# Do NOT run `vercel` from the repo root — that builds via turbo and can't find
# the adapter output ("No Output Directory named public"). The git integration
# is the only correct deploy path for this monorepo.
#
set -euo pipefail

ROOT="$(git -C "$(dirname "$0")" rev-parse --show-toplevel)"
git -C "$ROOT" push origin main
echo "Pushed. Vercel is building + auto-promoting to boringcomputers.com."
echo "Watch: https://vercel.com/goshen-labs/boring-computers"

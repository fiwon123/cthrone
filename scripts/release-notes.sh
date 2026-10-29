#!/usr/bin/env bash
#
# Print the "Changes" block for a release draft.
#
# GitHub's `releases/generate-notes` walks the commit graph from the previous
# release tag to the release commit and lists the pull requests it finds there.
# This repository squash-merges the release PR (`dev` -> `main`), so the feature
# commits stay on `dev` and never become ancestors of the squash commit: the
# generated notes see one commit, associated with the release PR, and every
# shipped change is missing.
#
# The list is therefore built from the pull requests merged into the
# integration branch, bounded by the previous release's commit and the release
# commit:
#
#   (previous release tag commit, release commit]
#
# so a PR merged into `dev` after the release was cut is not listed, and the
# release PR itself — based on `main` — never is.
#
# Usage:
#   scripts/release-notes.sh <owner/repo> <version> <target-commit> [<previous-tag>]
#
# Prints Markdown on stdout. Exits non-zero when the window cannot be
# determined, so the caller can fall back to a commit-list pointer.
#
# Requires an authenticated `gh` (GH_TOKEN or GITHUB_TOKEN in CI).
set -euo pipefail

usage="usage: release-notes.sh <owner/repo> <version> <target-commit> [<previous-tag>]"
if [ "$#" -lt 3 ]; then
  echo "$usage" >&2
  exit 2
fi
repo="$1"
version="$2"
commit="$3"
prev_tag="${4:-}"

# The branch every feature PR merges into. A variable so a future rename is one
# line rather than a hunt through the jq program.
base_branch="${RELEASE_BASE_BRANCH:-dev}"

if [ -z "$prev_tag" ]; then
  # Newest published (non-draft) release that is not the one being built. A
  # re-run of an existing draft must still look past it to the release before.
  prev_tag="$(
    gh api "repos/$repo/releases?per_page=30" \
      --jq "[.[] | select(.draft == false and .tag_name != \"$version\")][0].tag_name // empty"
  )"
fi
if [ -z "$prev_tag" ]; then
  echo "release-notes: no previous published release — cannot bound the changelog" >&2
  exit 1
fi

prev_at="$(gh api "repos/$repo/commits/$prev_tag" --jq '.commit.committer.date')"
end_at="$(gh api "repos/$repo/commits/$commit" --jq '.commit.committer.date')"
if [ -z "$prev_at" ] || [ -z "$end_at" ]; then
  echo "release-notes: could not resolve the window ($prev_tag..$commit)" >&2
  exit 1
fi

# Group by label: the repository maps every commit type to one (see
# .opencode/instructions/workflow.md). An unlabelled PR still lands in "Other
# changes" rather than disappearing from the notes.
jq_filter="$(cat <<'JQ'
def category:
  if   (.labels | map(.name) | index("enhancement"))   then "Features"
  elif (.labels | map(.name) | index("bug"))           then "Fixes"
  elif (.labels | map(.name) | index("documentation")) then "Documentation"
  elif (.labels | map(.name) | index("refactor"))      then "Refactoring"
  elif (.labels | map(.name) | index("test"))          then "Tests"
  elif (.labels | map(.name) | index("ci"))            then "CI"
  elif (.labels | map(.name) | index("dependencies"))  then "Dependencies"
  elif (.labels | map(.name) | index("chore"))         then "Maintenance"
  else "Other changes"
  end;

[ .[]
  | select(.mergedAt > "@PREV_AT@" and .mergedAt <= "@END_AT@")
  | . + {category: category}
]
| sort_by(.number)
| . as $prs
| ["Features", "Fixes", "Documentation", "Refactoring", "Tests", "CI",
   "Dependencies", "Maintenance", "Other changes"]
| map(
    . as $heading
    | [$prs[] | select(.category == $heading)] as $group
    | if ($group | length) == 0 then empty
      else
        "#### " + $heading + "\n\n"
        + ($group
           | map("- " + .title + " — [#" + (.number | tostring) + "](" + .url + ") by @" + (.author.login // "ghost"))
           | join("\n"))
      end
  )
| join("\n\n")
JQ
)"
jq_filter="${jq_filter//@PREV_AT@/$prev_at}"
jq_filter="${jq_filter//@END_AT@/$end_at}"

changes="$(
  gh pr list \
    --repo "$repo" \
    --state merged \
    --base "$base_branch" \
    --limit 200 \
    --search "merged:>=$prev_at" \
    --json number,title,url,author,labels,mergedAt \
    --jq "$jq_filter"
)"

if [ -z "$changes" ]; then
  echo "release-notes: no pull requests merged into $base_branch in ($prev_at, $end_at]" >&2
  changes="_No pull requests were merged into \`$base_branch\` in this release._"
fi

printf '%s\n\n' "$changes"
printf '**Full changelog**: https://github.com/%s/compare/%s...%s\n' "$repo" "$prev_tag" "$version"

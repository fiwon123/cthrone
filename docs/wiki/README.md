# Wiki source

These pages are the source of the GitHub wiki
(<https://github.com/fiwon123/cthrone/wiki>). They live in the repository so a
change to them is reviewed like any other, and so the wiki cannot drift from the
release it describes.

## Publishing

The wiki is a second git repository. GitHub creates it the first time a page is
made in the web UI; after that:

```bash
git clone https://github.com/fiwon123/cthrone.wiki.git
for page in docs/wiki/*.md; do
  case "$(basename "$page")" in
    README.md) continue ;;   # this file, not a page
  esac
  cp "$page" cthrone.wiki/
done
cd cthrone.wiki && git add -A && git commit -m "wiki: sync from docs/wiki" && git push
```

The file names are the page names: `Home.md` is the landing page, `_Sidebar.md`
is the navigation. `README.md` is this page's own source — it explains the
publishing, and it is not copied into the wiki.

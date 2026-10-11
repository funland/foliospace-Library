# Optional TMDB metadata: bring your own credential

[简体中文](tmdb-setup.zh-CN.md) · [Movie library](api/video-library-v1.md)

FolioSpace Library 1.03 can use TMDB for movie/TV candidate search, confirmed
metadata/artwork, and an explicit single-season episode-directory refresh.
TMDB does not supply the movie files. Local files, NFO, artwork and manual fields
remain usable without enabling any online provider.

## Register and configure

1. On a desktop browser, sign in to **your own** TMDB account and use
   [Settings → API](https://www.themoviedb.org/settings/api) to apply. Describe
   your actual use and review the current terms yourself; FolioSpace does not
   register, accept terms, or provide a shared developer credential for you.
2. This adapter expects TMDB's **API Read Access Token** (Bearer token), not the
   shorter v3 API key. It is also **not** your FolioSpace client-access token.
3. With the 1.03 Compose files, copy `.env.example` to your private `.env` if you
   have not already created one. Keep an existing `.env` and its other settings.
   Edit only these settings in your local editor:

   ```dotenv
   FOLIOSPACE_VIDEO_METADATA_PROVIDER=tmdb
   FOLIOSPACE_TMDB_TOKEN=YOUR_OWN_TMDB_API_READ_ACCESS_TOKEN
   FOLIOSPACE_TMDB_LANGUAGE=en-US
   ```

   The value shown is a placeholder, not a usable token. Use `zh-CN` if you prefer
   Simplified Chinese metadata. Both the explicit provider and nonempty token
   are required; shipped defaults are blank/disabled.
4. After backing up `/config`, apply your own deployment's configuration change
   with `docker compose up -d`. If you use `docker run`, pass the same variables
   using a protected `--env-file`; do not paste tokens into command arguments.
   Older custom Compose files must add the three environment mappings shown in
   the current `docker-compose.yml`; `.env` alone is not passed to containers.
5. Open Videos, select a file and its metadata editor, search for a title, review
   candidates, then confirm the intended match and request artwork explicitly.
   Incorrect matches can be corrected; a provider outage does not block local
   playback. Expected-episode refresh requires a confirmed TMDB series and a
   compatible client/API; it does not scan the entire library automatically.

## Privacy and operation

Keep `.env` owner-readable only (for example `chmod 600 .env`) and outside shared
folders. The repository and Docker build context ignore real `.env` files, but
that does not protect copies or backups. Docker administrators can inspect
container environment variables. Never publish credentials in Git, images,
screenshots, URLs, logs, support bundles, or rendered `docker compose config`
output; use `docker compose config --quiet` for validation. No credential belongs
in browser JavaScript or native-client preferences.

Search sends the entered query/type/language; metadata, image and season requests
send the chosen provider identifiers. The adapter does not upload your media
bytes or library paths. Requests and responses are bounded, with retry/cooldown
and expiry. The web **Clear TMDB cache** action removes provider records/artwork
without deleting your media, local sidecars or manual fields. Disable the provider
before clearing it for termination; review provider-data backups separately.
See the [cache and API contract](api/video-library-v1.md#optional-tmdb).

## Licensing and attribution are separate from credential ownership

TMDB's [official FAQ](https://developer.themoviedb.org/docs/faq) distinguishes
non-commercial use with attribution from commercial licensing. **Having your own
token does not itself authorize commercial use**, and FolioSpace's source-code
license does not license TMDB data/images. Assess the actual service/client use;
contact TMDB for applicable commercial permission. This guide is not a license
grant or a legal determination for your deployment.

Keep the approved TMDB logo and required notice in an About/Credits surface:

> This product uses the TMDB API but is not endorsed or certified by TMDB.

FolioSpace's web About and video-library footer provide attribution; independent
clients displaying TMDB data/artwork must address their own attribution and terms.
Use the [approved logos](https://www.themoviedb.org/about/logos-attribution) without
implying endorsement. Review the current [API terms](https://www.themoviedb.org/api-terms-of-use),
[registration guide](https://developer.themoviedb.org/docs/getting-started), and
[token documentation](https://developer.themoviedb.org/docs/authentication-application).
FAQ, registration and authentication guidance checked 2026-10-11; operators must
review the full current terms directly before enabling their own deployment.

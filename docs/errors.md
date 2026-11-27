# Error reference

<!-- Generated from errors.<code> locale keys. Run go run ./scripts/gen-errors. -->

| Code | Meaning | How to fix |
| --- | --- | --- |
| `api_incompatible` | The server answered in a format this version does not understand. | Update mothdex. |
| `canceled` | Canceled. | No action required; run the command again to retry. |
| `confirmation_required` | This command needs confirmation. | Add --force to skip the prompt. |
| `disk_full` | Not enough free space in [path] (needs [needed]). | Free some space or choose another folder with -o. |
| `exists` | [path] already exists. | Set download.on_exists to rename or overwrite. |
| `index_out_of_range` | Result #[index] does not exist (last search has [count]). | Pick a number between 1 and [count]. |
| `integrity` | The downloaded content failed integrity or origin verification. | Try again, or choose another link with --link. |
| `internal` | Unexpected error. | Details are in [log_path]. Please report it. |
| `invalid_config` | Invalid setting [key]=[value] (from [source]). | Run "mothdex config reset [key]". |
| `invalid_request` | Invalid request: [detail]. | Check the slug, filters and values you passed. |
| `network` | Cannot reach the Mothdex server. | Check your connection. |
| `no_session` | There is no previous search to refer to. | Run "mothdex search <text>" first, or use --slug. |
| `not_found` | No entry found for [ref]. | Check the spelling or search with fewer words. |
| `path_rejected` | Refusing to write to [path]. | Nothing was written. Choose another folder with -o, or check profile.folders. |
| `permission_denied` | Cannot write to [path]. | Pick another folder with -o, or fix its permissions. |
| `rate_limited` | The server asked us to slow down. | Wait [retry_after] and try again. |
| `size_mismatch` | The downloaded file has the wrong size ([got] of [expected]). | Try again, or choose another link with --link. |
| `unmapped_platform` | No folder is set for platform [platform]. | Pass -o <dir>, or set profile.folders.[platform] in the configuration. |
| `unsafe_name` | Refusing to save [name]: its file type can run code. | Nothing was written. Choose another link with --link, or pass --allow-executable if you trust it. |
| `untrusted_redirect` | The server redirected to [host], which is not trusted. | Nothing was sent there. Try again later; if it persists, report it. |
| `untrusted_url` | Refusing to download from [host]. | Only [allowed] is trusted. |
| `update_failed` | The update could not be completed. | Reinstall with the official command: [install_command] |
| `update_unavailable` | This installation cannot be updated by mothdex. | Install using the one-line command for your OS: [install_command] |
| `upstream` | The server had a problem (HTTP [status]). | Try again in a few minutes. |
| `usage` | Invalid usage: [detail] | Run "mothdex [command] --help". |

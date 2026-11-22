### Changed

- T-77: `api-client.md` §3 maps the upstream error codes (cavv-dev/mothdex-api `7a87af9`): the HTTP status decides
  first, then `info.error` as an exact code, with the old message form kept for backward compatibility; unknown codes
  are `KindAPI`; `RATE_LIMIT_ERROR` follows the 429 rules; `Error.Code` is new; `info.message` is untrusted, capped,
  logged sanitized and never shown as the error text.

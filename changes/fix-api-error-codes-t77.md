### Changed

- T-77: the API client reads the upstream error codes (cavv-dev/mothdex-api 7a87af9): `info.error` is a code and
  `info.message` the human text. `ENTRY_NOT_FOUND` is not found; `INVALID_API_VERSION` is incompatible;
  `MALFORMED_JSON`, `INVALID_JSON_OBJECT`, `MISSING_REQUIRED_FIELDS`, `INVALID_FIELD_TYPE` and `INVALID_INPUT` are
  invalid requests; `DB_OPERATION_ERROR`, `DB_ERROR` and `UNEXPECTED_ERROR` are upstream errors and are retried;
  `RATE_LIMIT_ERROR` takes the same wait and circuit as an HTTP 429 (it reads `Retry-After`); `HTTP_ERROR` and any
  unknown code are a generic API error. The old message-text form still works. The new `Error.Code` holds the raw code;
  the message is cleaned, capped at 512 bytes, kept out of the localized text, never used for logic, and capped at
  200 bytes in the verbose cause. The unused `info.code` forward-compat field is no longer read. No new i18n key: the
  user-facing text is still per error kind. A code in the body of any non-200 answer is recorded too (the status still
  decides the kind), and an explicit `info.error: null` is a decode failure, as in api-client 3.1.

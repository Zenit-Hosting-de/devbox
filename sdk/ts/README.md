# @zenit-hosting/devbox-sdk

TypeScript types for writing DevBox plugins. The types are generated from the plugin protocol schemas (`request`, `event`, `check-result`, `plugin`, `messages`) by `make generate` and exported under the `protocol` namespace:

```ts
import { protocol } from "@zenit-hosting/devbox-sdk";
```

Do not edit `src/gen/` by hand; change the schema in `schema/` and run `make generate`.

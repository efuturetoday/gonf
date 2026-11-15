# Changelog

## 0.1.0 (2026-11-15)


### ⚠ BREAKING CHANGES

* **boot:** WithoutDotenv replaces WithoutEnv
* factories bring own mapping, profile sources in phase 1
* functions replace secret handlers, add env, file, base64, uuid
* environment matched against mappings, no env lookup in core
* explanation carries the property of its key
* gonf.Bind is gone, use NewMapping and WithMappings. BindOption is now MappingOption.

### Features

* add Bind for typed structs ([928b2d9](https://github.com/efuturetoday/gonf/commit/928b2d9ac45d1c219b29a2e885c0a5fd58d25398))
* add Describe, README, license and CI ([a6719f4](https://github.com/efuturetoday/gonf/commit/a6719f4e863c05e43d2b3bca724f7e0b48d8849f))
* add Mapping registry ([55e819a](https://github.com/efuturetoday/gonf/commit/55e819a54b4936d317ede8d3549b774bc98f7386))
* add profile key and source factories ([5addbe8](https://github.com/efuturetoday/gonf/commit/5addbe860dd8aed3636660fda9f3fcad5d77ee02))
* add profiles, env key mapping and expansion ([b535464](https://github.com/efuturetoday/gonf/commit/b535464410cea2f2f4e2b9357589d3b4522a568d))
* add secret handlers and aes-gcm ([abf99ed](https://github.com/efuturetoday/gonf/commit/abf99edc4f10030f2cc8813afad3af743d637d4c))
* add Secret, Explain and Explanations ([6efdf7c](https://github.com/efuturetoday/gonf/commit/6efdf7cbd29f2b801061be746a952015d7a6b96d))
* add sources, ordinal merge and origins ([ea971db](https://github.com/efuturetoday/gonf/commit/ea971db39e4eb83c789e843c87c5a86c7673fdb5))
* add Watch for live reload ([0199043](https://github.com/efuturetoday/gonf/commit/01990430f727982aeb131dc56d651bd6b07908cb))
* add WithConverter ([7b9f890](https://github.com/efuturetoday/gonf/commit/7b9f890103806c2c656f97c2aea7b8882d10f646))
* **boot:** add Describe ([a773283](https://github.com/efuturetoday/gonf/commit/a77328381c85bfe122cf7cac013e654bf6a4c8e0))
* **boot:** add Watch, reload guide and example ([244f8c1](https://github.com/efuturetoday/gonf/commit/244f8c14f0393fc4ef45876ab7926ec6486c4318))
* **boot:** decrypt aes-gcm values by default ([9a63c87](https://github.com/efuturetoday/gonf/commit/9a63c8727bc7d4c729856051a3d98b1f98713185))
* **boot:** load config by convention ([d6dd173](https://github.com/efuturetoday/gonf/commit/d6dd1733f0eb96b5675dafa6433c98c6aef59b7d))
* **boot:** WithDir and WithoutEnv for tests ([f31121e](https://github.com/efuturetoday/gonf/commit/f31121eff661f6596bf877e6eb0ed41f3ba1753b))
* **boot:** WithoutDotenv replaces WithoutEnv ([8b1851a](https://github.com/efuturetoday/gonf/commit/8b1851a5e01496f0dfcda0a6c0695aab7b17446b))
* **dir:** add directory source ([49813f6](https://github.com/efuturetoday/gonf/commit/49813f69dc2cbf3d51d7f3ec5225543696439430))
* Encode and Escape ([90387e2](https://github.com/efuturetoday/gonf/commit/90387e2f8652bf4db64402dbcc09ebba4c75c42c))
* environment matched against mappings, no env lookup in core ([bc622a3](https://github.com/efuturetoday/gonf/commit/bc622a3df358753937af7ceca6990c04ae9bf31a))
* factories bring own mapping, profile sources in phase 1 ([47b102d](https://github.com/efuturetoday/gonf/commit/47b102db03752e8cb5017b8461b51746faaffd9a))
* **flag:** add flag source ([818e90e](https://github.com/efuturetoday/gonf/commit/818e90e15d3bde06b0173b56367af85bd388bd13))
* **flag:** bool flags ([981efec](https://github.com/efuturetoday/gonf/commit/981efec4a4f2c5ec53cb263b9749c1de8196ab77))
* functions git and slug ([14a4769](https://github.com/efuturetoday/gonf/commit/14a47699dd266f24107ec2c20693872ee61aedb3))
* functions replace secret handlers, add env, file, base64, uuid ([813207d](https://github.com/efuturetoday/gonf/commit/813207d97a69444cdffcf62b80dd641d552d6b30))
* **git:** the remote as a web address ([30cab05](https://github.com/efuturetoday/gonf/commit/30cab05b7cec7e83de3d0bb9d8872c12cc596d3f))
* point defaults at their NewMapping ([47a0f69](https://github.com/efuturetoday/gonf/commit/47a0f69165422cf1f42daed00dac8b89c88093bc))
* reload options in boot, secret and source fixes, docs ([d77d67b](https://github.com/efuturetoday/gonf/commit/d77d67bb21faf4953bf84d72364addc9357d6a56))
* report fields that refer to secrets ([5dd8c30](https://github.com/efuturetoday/gonf/commit/5dd8c3044b3a14eed25caa69a4b498ee4a2fa018))
* **schema:** json schema from describe ([c45020b](https://github.com/efuturetoday/gonf/commit/c45020bc1e5e06d75b13b6eff1fecda35d66bd49))
* **schema:** keep mappings with allow unknown open ([66c40a1](https://github.com/efuturetoday/gonf/commit/66c40a1bf826f3e6332c167d5cee1d8beb5f1b9a))
* **source:** report document changes by polling ([af0f6fd](https://github.com/efuturetoday/gonf/commit/af0f6fd1c7ab108698ef42cc7682d805b1b7e3cf))
* **sourcetest:** the contract of a Source ([d254b35](https://github.com/efuturetoday/gonf/commit/d254b35c49db997b0ae2a9d7257c41f4e008391c))
* **source:** Verbatim, values as text ([5cd2761](https://github.com/efuturetoday/gonf/commit/5cd2761d30ec1f47140c8e71f8857117bbae32d0))
* validate structs, report unknown keys ([5448355](https://github.com/efuturetoday/gonf/commit/54483556794ff262e1ceb14b26544445d50bbad0))
* warnings, sources and env name conflicts ([f478d07](https://github.com/efuturetoday/gonf/commit/f478d07b42356d7d39875fd08626bf2f14a1a47e))
* **yaml:** add YAML format module ([1745aa0](https://github.com/efuturetoday/gonf/commit/1745aa0e7a529d0fe806ef4d8eb0406fe769975f))


### Bug Fixes

* copy references in Mapping.Get ([06994ef](https://github.com/efuturetoday/gonf/commit/06994efae6c35551cc5e6e618d552deaeab9f625))
* **examples:** greeting of reload back to hello ([725d6eb](https://github.com/efuturetoday/gonf/commit/725d6ebba26e8a7ece1e2e0b61a3b21844bfe3f6))
* **git:** errcheck on the hash of changes ([ec3c5a7](https://github.com/efuturetoday/gonf/commit/ec3c5a7e8c24b7a659f1925ba3fc37d53fc8ce11))
* **json:** limit nesting on every Go version ([eb8fb73](https://github.com/efuturetoday/gonf/commit/eb8fb73f756e6080042583eb60e4348082e09513))
* **json:** report duplicate keys with lines ([759138c](https://github.com/efuturetoday/gonf/commit/759138cf5e33c965e915057862e038992749515d))
* limit expansion per Get ([b1f3e7c](https://github.com/efuturetoday/gonf/commit/b1f3e7cf6c54656abcd75009ff0a89c1e4a80774))
* name origin of unregistered mapping ([55e70e9](https://github.com/efuturetoday/gonf/commit/55e70e98af53daa84c8cceaebe07f252651dbcb6))
* reject ambiguous leading zeros in integers ([9ec44c5](https://github.com/efuturetoday/gonf/commit/9ec44c52bc95e24484456446ef470adf9b212c08))


### Code Refactoring

* explanation carries the property of its key ([d844ac7](https://github.com/efuturetoday/gonf/commit/d844ac7694e525f681260a9320a954e01f3bc629))
* remove Bind, Mappings are the only way ([77079ba](https://github.com/efuturetoday/gonf/commit/77079ba682b7b1476266420e04e130326c9c4ee8))

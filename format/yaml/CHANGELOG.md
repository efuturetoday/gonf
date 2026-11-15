# Changelog

## [0.2.0](https://github.com/efuturetoday/gonf/compare/format/yaml/v0.1.0...format/yaml/v0.2.0) (2026-11-15)


### ⚠ BREAKING CHANGES

* gonf.Bind is gone, use NewMapping and WithMappings. BindOption is now MappingOption.

### Features

* report fields that refer to secrets ([5dd8c30](https://github.com/efuturetoday/gonf/commit/5dd8c3044b3a14eed25caa69a4b498ee4a2fa018))
* **yaml:** add YAML format module ([1745aa0](https://github.com/efuturetoday/gonf/commit/1745aa0e7a529d0fe806ef4d8eb0406fe769975f))


### Bug Fixes

* format/yaml and cmd/gonf on gonf v0.1.0 ([d4eeefe](https://github.com/efuturetoday/gonf/commit/d4eeefe6d45a75547ba09aaeff4680e4b850b9ea))


### Code Refactoring

* remove Bind, Mappings are the only way ([77079ba](https://github.com/efuturetoday/gonf/commit/77079ba682b7b1476266420e04e130326c9c4ee8))

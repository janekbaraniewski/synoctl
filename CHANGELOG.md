# Changelog

## [0.2.8](https://github.com/janekbaraniewski/synoctl/compare/v0.2.7...v0.2.8) (2026-05-20)


### Documentation

* expand landing page screens ([#43](https://github.com/janekbaraniewski/synoctl/issues/43)) ([6e006f8](https://github.com/janekbaraniewski/synoctl/commit/6e006f8f18aebad26dde0e43f89906d470615a15))

## [0.2.7](https://github.com/janekbaraniewski/synoctl/compare/v0.2.6...v0.2.7) (2026-05-19)


### Bug Fixes

* **release:** include docs changes in release-please ([3c8c255](https://github.com/janekbaraniewski/synoctl/commit/3c8c2557601aaf6a76d0a394fe4ec81ee4a9f137))


### Documentation

* add transparent synoctl logo ([19d7c5e](https://github.com/janekbaraniewski/synoctl/commit/19d7c5ec728940c132104817617c480992055e2b))

## [0.2.6](https://github.com/janekbaraniewski/synoctl/compare/v0.2.5...v0.2.6) (2026-05-19)


### Features

* **tui:** improve storage and FileStation flows ([be8f92b](https://github.com/janekbaraniewski/synoctl/commit/be8f92bf012593058177ba905c77c8e0571699f8))


### Bug Fixes

* deploy Pages and repair DSM package installs ([225099f](https://github.com/janekbaraniewski/synoctl/commit/225099f30b43f406fe8fef5543f1de33a55f1888))
* **docs:** tighten landing page fit and autoplay ([#36](https://github.com/janekbaraniewski/synoctl/issues/36)) ([c019d3f](https://github.com/janekbaraniewski/synoctl/commit/c019d3fb0806c46d7f5030433eb1e01a44f7cb46))
* **security:** harden GitHub security posture ([5f2e538](https://github.com/janekbaraniewski/synoctl/commit/5f2e538b734938bc9fa75fc1c47d491f925f07e3))


### Dependencies

* **deps:** bump github.com/charmbracelet/x/ansi from 0.11.6 to 0.11.7 in the go-minor-and-patch group ([#28](https://github.com/janekbaraniewski/synoctl/issues/28)) ([5351991](https://github.com/janekbaraniewski/synoctl/commit/53519917d72c9b9b30d76ea1547bb30a2a5dda72))

## [0.2.5](https://github.com/janekbaraniewski/synoctl/compare/v0.2.4...v0.2.5) (2026-05-18)


### Bug Fixes

* **ci:** drop golang/govulncheck-action to escape transitive tag refs ([ff81c88](https://github.com/janekbaraniewski/synoctl/commit/ff81c885a302c93e8b36c51f18ae75d5045b756d))
* **ci:** grant write perms to dependabot rebase + skip scorecard publish on push ([774a3e3](https://github.com/janekbaraniewski/synoctl/commit/774a3e37e8722e48e1d538ff74bc4219a530fc31))
* **ci:** pin remaining actions to full commit SHAs ([cb83c67](https://github.com/janekbaraniewski/synoctl/commit/cb83c6778186ca54993bb26cfbfc35da3a55452f))
* **ci:** restore golang/govulncheck-action ([5740989](https://github.com/janekbaraniewski/synoctl/commit/57409899a58ce980785dd4a63456644d6ec7f8fc))

## [0.2.4](https://github.com/janekbaraniewski/synoctl/compare/v0.2.3...v0.2.4) (2026-05-18)


### Features

* **settings:** DSM update / time / power / external access views ([e8c0de0](https://github.com/janekbaraniewski/synoctl/commit/e8c0de07635f86026d6f7deba2f37f6cfdfd23e8))


### Bug Fixes

* **ci:** drop hard AUTOMATION_TOKEN requirement from dependabot refresh ([1b1ba69](https://github.com/janekbaraniewski/synoctl/commit/1b1ba69dcbb533fb3e47a9f9a77ba1935846b618))

## [0.2.3](https://github.com/janekbaraniewski/synoctl/compare/v0.2.2...v0.2.3) (2026-05-18)


### Features

* **backup:** run-now and suspend/resume for Hyper Backup + Active Backup ([207b5b1](https://github.com/janekbaraniewski/synoctl/commit/207b5b15d5926c46b05e2f49279a9c6bcd02e262))
* **containers:** tabbed Containers / Images / Networks view ([f8ccb5e](https://github.com/janekbaraniewski/synoctl/commit/f8ccb5ef79639ec1bfd9a1aa8b98caf171c378e5))
* **coverage:** Virtual Machine Manager + iSCSI/SAN views ([1d3856a](https://github.com/janekbaraniewski/synoctl/commit/1d3856afc99315e49f217d644d3942ed5fe88508))
* **monitor:** Resource Monitor view with historical CPU/mem/net/disk charts ([34b4957](https://github.com/janekbaraniewski/synoctl/commit/34b49577563b2529151404e7e1e112078998a52d))

## [0.2.2](https://github.com/janekbaraniewski/synoctl/compare/v0.2.1...v0.2.2) (2026-05-18)


### Features

* **cloudsync:** add Cloud Sync tasks view ([ea7fd5a](https://github.com/janekbaraniewski/synoctl/commit/ea7fd5a5ed66e4040c61d402be16126052c89691))
* **coverage:** notifications + quotas views ([12a85e2](https://github.com/janekbaraniewski/synoctl/commit/12a85e2d5405eb5fa30d2e227cc4793c54283365))
* **writes:** firewall + DDNS + scheduled-task mutations ([f075e39](https://github.com/janekbaraniewski/synoctl/commit/f075e3941f979b784758da3d994bfd04cd6407dc))

## [0.2.1](https://github.com/janekbaraniewski/synoctl/compare/v0.2.0...v0.2.1) (2026-05-18)


### Features

* **demo:** synoctl demo — full TUI against canned data for screenshots ([#6](https://github.com/janekbaraniewski/synoctl/issues/6)) ([0a3791d](https://github.com/janekbaraniewski/synoctl/commit/0a3791d2f1273343829e5c2e970d744b96b1deef))
* **shares:** list, create, and delete snapshots from the Shares view ([91105bb](https://github.com/janekbaraniewski/synoctl/commit/91105bbe546df6d3a6f0baea08680ecc600f4f2b))

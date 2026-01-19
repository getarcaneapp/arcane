## v1.13.2

### Backend - Bug fixes

* apply auth check before proxying request to environments ([#1532](https://github.com/getarcaneapp/arcane/pull/1532) by @kmendell)
* allow HTTP_PROXY and HTTPS_PROXY environment variables ([#1534](https://github.com/getarcaneapp/arcane/pull/1534) by @kmendell)
* use image pull timeout for project pull ([#1533](https://github.com/getarcaneapp/arcane/pull/1533) by @kmendell)
* update color of port badge to be more distinguishable([b0e8b54](https://github.com/getarcaneapp/arcane/commit/b0e8b54ec7c416ef089476106f23c365a74724cd) by @kmendell)

### Dependencies

* bump go version to 1.25.6([501baaf](https://github.com/getarcaneapp/arcane/commit/501baaf7708e8fc83b030650abd04919880da2e4) by @kmendell)
* bump pnpm to 10.28.1([c5ef93e](https://github.com/getarcaneapp/arcane/commit/c5ef93e54db76e44b932953af9f8303868fb52c0) by @kmendell)



**Full Changelog**: https://github.com/getarcaneapp/arcane/compare/v1.13.1...v1.13.2

## v1.13.1

### Backend - Bug fixes

* ability to resize editor panels horizontally ([#1500](https://github.com/getarcaneapp/arcane/pull/1500) by @kmendell)
* allow oidc endpoints to be defined manually ([#1510](https://github.com/getarcaneapp/arcane/pull/1510) by @kmendell)
* remove file line from db debug logs([fbe204c](https://github.com/getarcaneapp/arcane/commit/fbe204c5ce919282a65313cfc0c889b763eebd64) by @kmendell)
* self update binary path for remote envrionments([974c675](https://github.com/getarcaneapp/arcane/commit/974c675550a0d5408f662d13fe3f8b07edb2267e) by @kmendell)
* generic webhooks do not allow ports ([#1517](https://github.com/getarcaneapp/arcane/pull/1517) by @kmendell)
* logo color not applying on refreshes([fe53985](https://github.com/getarcaneapp/arcane/commit/fe539851d621a35c1ebaa08217151e65bbaae64c) by @kmendell)

### Dependencies

* bump @sveltejs/kit from 2.49.4 to 2.49.5 in the npm_and_yarn group across 1 directory ([#1492](https://github.com/getarcaneapp/arcane/pull/1492) by @dependabot[bot])
* bump svelte from 5.46.3 to 5.46.4 in the npm_and_yarn group across 1 directory ([#1494](https://github.com/getarcaneapp/arcane/pull/1494) by @dependabot[bot])
* bump devalue to 5.6.2([315b459](https://github.com/getarcaneapp/arcane/commit/315b4595c5239bd14e66d0e5ed394cb2a54aa3f4) by @kmendell)
* bump prettier from 3.7.4 to 3.8.0 ([#1507](https://github.com/getarcaneapp/arcane/pull/1507) by @dependabot[bot])
* bump github.com/danielgtaylor/huma/v2 from 2.34.1 to 2.34.2 in /backend ([#1504](https://github.com/getarcaneapp/arcane/pull/1504) by @dependabot[bot])
* bump github.com/sirupsen/logrus from 1.9.3 to 1.9.4 in /cli ([#1503](https://github.com/getarcaneapp/arcane/pull/1503) by @dependabot[bot])

### Other

* fix context patterns through codebase ([#1450](https://github.com/getarcaneapp/arcane/pull/1450) by @kmendell)
* cleanup unused setting key/values on startup ([#1519](https://github.com/getarcaneapp/arcane/pull/1519) by @kmendell)
* run workflows on release/** branches([4ac7e3e](https://github.com/getarcaneapp/arcane/commit/4ac7e3ea614966747064d0610434039a30056bbc) by @kmendell)



**Full Changelog**: https://github.com/getarcaneapp/arcane/compare/v1.13.0...v1.13.1

## v1.13.0

### Backend - New features

* allow sensitive env variables to be read from _FILE ([#1423](https://github.com/getarcaneapp/arcane/pull/1423) by @kmendell)
* add JSON parsing and structured log display functionality ([#1463](https://github.com/getarcaneapp/arcane/pull/1463) by @FusionStreak)
* use shoutrrr for notifications (apprise deprecated) ([#1424](https://github.com/getarcaneapp/arcane/pull/1424) by @kmendell)
* bulk actions for containers and projects ([#1466](https://github.com/getarcaneapp/arcane/pull/1466) by @kmendell)
* auto-prune/prune scheduler job ([#1467](https://github.com/getarcaneapp/arcane/pull/1467) by @kmendell)
* project status filter selector ([#1484](https://github.com/getarcaneapp/arcane/pull/1484) by @kmendell)

### Backend - Bug fixes

* add option for ssh host key verification and known hosts ([#1443](https://github.com/getarcaneapp/arcane/pull/1443) by @kmendell)
* network overview not reload with new data([c6bbbc7](https://github.com/getarcaneapp/arcane/commit/c6bbbc781e297bdb0711d2042263f1c59ec18526) by @kmendell)
* git sync interval incorrect ([#1446](https://github.com/getarcaneapp/arcane/pull/1446) by @kmendell)
* oidc insecure tls not working for self signed CA ([#1448](https://github.com/getarcaneapp/arcane/pull/1448) by @kmendell)
* remove updater lifecycle hooks ([#1468](https://github.com/getarcaneapp/arcane/pull/1468) by @kmendell)
* margin not being applied on resource pages([dc10a3c](https://github.com/getarcaneapp/arcane/commit/dc10a3c3fd9d7fbfd5e317025afd3c18d50a48de) by @kmendell)
* unable to save env in git synced project ([#1471](https://github.com/getarcaneapp/arcane/pull/1471) by @kmendell)
* invalid api input for container creation ([#1483](https://github.com/getarcaneapp/arcane/pull/1483) by @kmendell)

### Dependencies

* bump @shikijs/monaco from 3.20.0 to 3.21.0 ([#1432](https://github.com/getarcaneapp/arcane/pull/1432) by @dependabot[bot])
* bump @react-email/components from 1.0.3 to 1.0.4 ([#1431](https://github.com/getarcaneapp/arcane/pull/1431) by @dependabot[bot])
* bump golang.org/x/term from 0.38.0 to 0.39.0 in /cli ([#1425](https://github.com/getarcaneapp/arcane/pull/1425) by @dependabot[bot])
* bump golang.org/x/text from 0.32.0 to 0.33.0 in /backend ([#1426](https://github.com/getarcaneapp/arcane/pull/1426) by @dependabot[bot])
* bump golang.org/x/mod from 0.31.0 to 0.32.0 in /backend ([#1429](https://github.com/getarcaneapp/arcane/pull/1429) by @dependabot[bot])
* bump github.com/goccy/go-yaml from 1.19.1 to 1.19.2 in /backend ([#1428](https://github.com/getarcaneapp/arcane/pull/1428) by @dependabot[bot])
* bump @sveltejs/kit from 2.49.3 to 2.49.4 ([#1479](https://github.com/getarcaneapp/arcane/pull/1479) by @dependabot[bot])
* bump golang.org/x/net from 0.48.0 to 0.49.0 in /backend ([#1475](https://github.com/getarcaneapp/arcane/pull/1475) by @dependabot[bot])
* bump svelte from 5.46.1 to 5.46.3 ([#1478](https://github.com/getarcaneapp/arcane/pull/1478) by @dependabot[bot])
* upgrade pnpm to 10.28.0([9804c5c](https://github.com/getarcaneapp/arcane/commit/9804c5c8fad49c25ba732b9bda0cc6a9495ad342) by @kmendell)

### Other

* consolidate and cleanup backend utils ([#1419](https://github.com/getarcaneapp/arcane/pull/1419) by @kmendell)
* remove enabled field from git syncs ([#1457](https://github.com/getarcaneapp/arcane/pull/1457) by @kmendell)
* only run deadcode wf on org branches([aac9ba3](https://github.com/getarcaneapp/arcane/commit/aac9ba3f90823f97f36d1db217f74426a0f88549) by @kmendell)
* update environment page to use settings form util ([#1469](https://github.com/getarcaneapp/arcane/pull/1469) by @kmendell)



**Full Changelog**: https://github.com/getarcaneapp/arcane/compare/v1.12.2...v1.13.0

## v1.12.2

### Backend - Bug fixes

* template editor heights being cutoff([7057deb](https://github.com/getarcaneapp/arcane/commit/7057deb42174cef218c623b1c431546c4a771396) by @kmendell)
* double label text on template buttons([6316833](https://github.com/getarcaneapp/arcane/commit/6316833c79f5b3e17c194c701ddc1446cab0b038) by @kmendell)
* use full absolute path for projects directory ([#1409](https://github.com/getarcaneapp/arcane/pull/1409) by @kmendell)
* editor cursor misalignment ([#1412](https://github.com/getarcaneapp/arcane/pull/1412) by @kmendell)



**Full Changelog**: https://github.com/getarcaneapp/arcane/compare/v1.12.1...v1.12.2

## v1.12.1

### Backend - Bug fixes

* include file editors not scrolling([52a004e](https://github.com/getarcaneapp/arcane/commit/52a004ea4869489091dca1efba0be0e1dccdd7b0) by @kmendell)
* resources not respecting multiple filters ([#1391](https://github.com/getarcaneapp/arcane/pull/1391) by @kmendell)
* dont remove .env content from git sync([95b7ac2](https://github.com/getarcaneapp/arcane/commit/95b7ac2982c19ec2d31b6843256a14c198101c93) by @kmendell)
* use unique icon for redeploy([84210cc](https://github.com/getarcaneapp/arcane/commit/84210cc3acf6864566153a8cc77f6397b38b44a3) by @kmendell)
* predefined networks being incorrectly sorted ([#1395](https://github.com/getarcaneapp/arcane/pull/1395) by @kmendell)
* volume size and created not sorting correctly([dc7f88c](https://github.com/getarcaneapp/arcane/commit/dc7f88c0ea1b33632f2a3dc49d9001c524ffc35a) by @kmendell)
* git repository sheet layout being cutoff([9052c4b](https://github.com/getarcaneapp/arcane/commit/9052c4b0ff6dd8c0de8a3ad325c5a9b10af04566) by @kmendell)
* disable font ligatures([f4c07a1](https://github.com/getarcaneapp/arcane/commit/f4c07a11bb16db51725bc943df83c77fff454f28) by @kmendell)



**Full Changelog**: https://github.com/getarcaneapp/arcane/compare/v1.12.0...v1.12.1

## v1.12.0

### Backend - New features

* show current environment in page title ([#1336](https://github.com/getarcaneapp/arcane/pull/1336) by @cabaucom376)
* show container stats inline in table ([#1359](https://github.com/getarcaneapp/arcane/pull/1359) by @kmendell)
* git repo sync for compose projects ([#1089](https://github.com/getarcaneapp/arcane/pull/1089) by @kmendell)

### Backend - Bug fixes

* networks unable to be searched or filtered([9c1be90](https://github.com/getarcaneapp/arcane/commit/9c1be901f1e5bac175c9f93314deaae48a14e256) by @kmendell)
* use full compose dependency tree for compose projects ([#1242](https://github.com/getarcaneapp/arcane/pull/1242) by @kmendell)
* add width constraint on mobile floating nav ([#1329](https://github.com/getarcaneapp/arcane/pull/1329) by @lucolvin)
* autocomplete inconsistencies in compose editor([2b82e7f](https://github.com/getarcaneapp/arcane/commit/2b82e7fb6e7d326cd763676515bb368f3d8395b5) by @kmendell)
* select all missing from context menu on editors([2cd8012](https://github.com/getarcaneapp/arcane/commit/2cd8012c3a20a9b8da9560e26751b7cfd8ca5ed5) by @kmendell)
* volumes unable to be sorted by size([07f8bae](https://github.com/getarcaneapp/arcane/commit/07f8baef275c7a79e0a7dc843aca243f0f9865cc) by @kmendell)
* mark remote environment online after successful agent handshake ([#1335](https://github.com/getarcaneapp/arcane/pull/1335) by @agent0x21)
* add FILE_PERM and DIR_PERM env variables ([#1334](https://github.com/getarcaneapp/arcane/pull/1334) by @kmendell)
* standard users able to perform admin only actions ([#1344](https://github.com/getarcaneapp/arcane/pull/1344) by @kmendell)
* projects directory not loaded from env or refreshed ([#1345](https://github.com/getarcaneapp/arcane/pull/1345) by @kmendell)
* refactor updater to have safer and more consistent upgrades ([#1315](https://github.com/getarcaneapp/arcane/pull/1315) by @kmendell)
* use built in cli to upgrade arcane ([#1358](https://github.com/getarcaneapp/arcane/pull/1358) by @kmendell)
* pull and redeploy button not working([1d8bdeb](https://github.com/getarcaneapp/arcane/commit/1d8bdeb5b52f586c9f603f85fa850a1591aeda4c) by @kmendell)
* truncate long headers on event dialog([9cedb5a](https://github.com/getarcaneapp/arcane/commit/9cedb5a6d4537cf5df725f6aed8c179c1428159b) by @kmendell)
* only send populated fields on container creation([c2371d7](https://github.com/getarcaneapp/arcane/commit/c2371d72bb7401507750fa4121db21c5ec61b32b) by @kmendell)
* duplicated save text on defautl templates page([a83f87b](https://github.com/getarcaneapp/arcane/commit/a83f87bc5cbc94384456a4708b715de13024fe85) by @kmendell)
* path mapping fix for projects directory on windows ([#1360](https://github.com/getarcaneapp/arcane/pull/1360) by @kmendell)
* api reference link showing incorrectly([e947e17](https://github.com/getarcaneapp/arcane/commit/e947e1776c6f4b605353e2e0a021defd46476e45) by @kmendell)
* auto update job not running([41d8a65](https://github.com/getarcaneapp/arcane/commit/41d8a6596c83508ef92ee464561cfbb1674d1e53) by @kmendell)

### Dependencies

* bump github.com/shirou/gopsutil/v4 from 4.25.11 to 4.25.12 in /backend ([#1322](https://github.com/getarcaneapp/arcane/pull/1322) by @dependabot[bot])
* bump golang.org/x/net from 0.47.0 to 0.48.0 in /backend ([#1323](https://github.com/getarcaneapp/arcane/pull/1323) by @dependabot[bot])
* bump @react-email/components from 1.0.2 to 1.0.3 ([#1317](https://github.com/getarcaneapp/arcane/pull/1317) by @dependabot[bot])
* bump github.com/samber/slog-gin from 1.18.0 to 1.19.1 in /backend ([#1371](https://github.com/getarcaneapp/arcane/pull/1371) by @dependabot[bot])
* bump @sveltejs/kit from 2.49.2 to 2.49.3 ([#1373](https://github.com/getarcaneapp/arcane/pull/1373) by @dependabot[bot])
* bump shiki from 3.20.0 to 3.21.0 ([#1374](https://github.com/getarcaneapp/arcane/pull/1374) by @dependabot[bot])
* upgrade go modules to version 1.25.5([87cf2c5](https://github.com/getarcaneapp/arcane/commit/87cf2c5ca0bfbdf9bdd437d0da4ffeb807de6e14) by @kmendell)

### Other

* ui tweaks and consistent styling across pages  ([#1210](https://github.com/getarcaneapp/arcane/pull/1210) by @cabaucom376)
* move general settings to job scheduler category ([#1310](https://github.com/getarcaneapp/arcane/pull/1310) by @kmendell)
* set glassUiEnabled to true by default([172a2fb](https://github.com/getarcaneapp/arcane/commit/172a2fb342c613890a680cd03834a1f7f63cd423) by @kmendell)
* set projectsDirectory to /app/data/projects by default([3c63bd0](https://github.com/getarcaneapp/arcane/commit/3c63bd0525f67506b70216dde94b86cdc6318eb1) by @kmendell)
* set diskUsagePath to /app/data/projects by default([607920e](https://github.com/getarcaneapp/arcane/commit/607920e2f08e26747aa8aa795a7931205356aad9) by @kmendell)



**Full Changelog**: https://github.com/getarcaneapp/arcane/compare/v1.11.3...v1.12.0

## v1.11.3

### Backend - Bug fixes

* checkbox mismatch between glass and non glass ui mode([a5a1d98](https://github.com/getarcaneapp/arcane/commit/a5a1d982acd49bb9c5f2cdac79dc2d458b436b69) by @kmendell)
* inconsistent sorting on db resources, casuing sorting issue in the frontend([3aa60e8](https://github.com/getarcaneapp/arcane/commit/3aa60e82fb6fc8e1aac7034d59205e48771b4291) by @kmendell)
* security agents false postive for malicious execution([baca054](https://github.com/getarcaneapp/arcane/commit/baca0540b342a3fdfee3a5fdc4a93f791d747b39) by @kmendell)
* show confirmation dialog when removing volumes([037bcf6](https://github.com/getarcaneapp/arcane/commit/037bcf69ec10414be9a45d914e6f9e8861907926) by @kmendell)
* add environment selector on error page to avoid a 'stuck state' ([#1298](https://github.com/getarcaneapp/arcane/pull/1298) by @kmendell)
* allow skipping tls verification for oidc endpoints ([#1300](https://github.com/getarcaneapp/arcane/pull/1300) by @kmendell)
* disable copy button on non-ssl instances ([#1309](https://github.com/getarcaneapp/arcane/pull/1309) by @kmendell)
* notifications on remote environments used incorrect urls ([#1302](https://github.com/getarcaneapp/arcane/pull/1302) by @kmendell)

### CLI - Bug fixes

* update table library and fix column header alignment([a83d610](https://github.com/getarcaneapp/arcane/commit/a83d6108e0ddfe9ceb449b30901dc51ec8bb031b) by @kmendell)

### Dependencies

* bump @xterm/xterm from 5.5.0 to 6.0.0 ([#1255](https://github.com/getarcaneapp/arcane/pull/1255) by @dependabot[bot])
* bump github.com/schollz/progressbar/v3 from 3.18.0 to 3.19.0 in /cli ([#1258](https://github.com/getarcaneapp/arcane/pull/1258) by @dependabot[bot])
* upgrade pnpm to 10.27.0([e2efb3a](https://github.com/getarcaneapp/arcane/commit/e2efb3ad88b001563be31feb62798e7a19aea938) by @kmendell)



**Full Changelog**: https://github.com/getarcaneapp/arcane/compare/v1.11.2...v1.11.3

## v1.11.2

### Backend - Bug fixes

* use both legacy and new api key headers for agent authentication ([#1271](https://github.com/getarcaneapp/arcane/pull/1271) by @kmendell)
* button flex 'bouncy' layout issue([51977fc](https://github.com/getarcaneapp/arcane/commit/51977fc0a8cbb58f30afa2ce5e62d00895424bd1) by @kmendell)
* add generate command back to built in cli([875a96a](https://github.com/getarcaneapp/arcane/commit/875a96a5ec7519cc4e9e9060ec53bc1399c4c257) by @kmendell)
* correctly fetch dynamic image/container items on dashboard([7965948](https://github.com/getarcaneapp/arcane/commit/7965948fc3d726fcd40144d7a9feb568ceb26913) by @kmendell)
* allow litteral quotes in oidc env values ([#1272](https://github.com/getarcaneapp/arcane/pull/1272) by @kmendell)



**Full Changelog**: https://github.com/getarcaneapp/arcane/compare/v1.11.1...v1.11.2

## v1.11.1

### Backend - Bug fixes

* api key validator not checking for user correctly for admin endpoints([dad577a](https://github.com/getarcaneapp/arcane/commit/dad577a4c07ec0d4e1b2a84ebd3de63ac37f02a4) by @kmendell)
* oidc form incorrectly parsing numbers preventing saving ([#1234](https://github.com/getarcaneapp/arcane/pull/1234) by @kmendell)
* use png logo for emails instead of svg([c2948d5](https://github.com/getarcaneapp/arcane/commit/c2948d5524b04a120b593c6fd3377aa27d47b1f2) by @kmendell)
* make inline start/stop icons colored for better readability([18de25c](https://github.com/getarcaneapp/arcane/commit/18de25cb3b24c2ef7f822735d79a9b737500c8d6) by @kmendell)
* dark/light mode not being respected on certain elements([b877daa](https://github.com/getarcaneapp/arcane/commit/b877daaad65ae7541e7e12ee56c80d26050ab1d3) by @kmendell)
* editing a user fails to save due to validation error([4203424](https://github.com/getarcaneapp/arcane/commit/4203424d0ec9b7afda37a1e282b03dc737d3f564) by @kmendell)
* change email encoding to quoted-printable ([#1238](https://github.com/getarcaneapp/arcane/pull/1238) by @kmendell)
* use correct data path and port in deployment snippets([cb4163d](https://github.com/getarcaneapp/arcane/commit/cb4163d9638543dbeed96867b978101e7536f01e) by @kmendell)
* incorrect api-key validation([cfaf05f](https://github.com/getarcaneapp/arcane/commit/cfaf05f6b62020f2a444877b33208c69618b0d36) by @kmendell)
* dashboard not pulling correct data on env switch([db53bb5](https://github.com/getarcaneapp/arcane/commit/db53bb59d1bfaadf08b91b14ebe727839343a6cf) by @kmendell)
* route matching for resource detail pages when switching environments([db552f5](https://github.com/getarcaneapp/arcane/commit/db552f546a680541e4167d571936e79d58ea32b2) by @kmendell)
* env values being injected into all containers by default ([#1250](https://github.com/getarcaneapp/arcane/pull/1250) by @kmendell)

### CLI - Bug fixes

* use proper timeouts for specific commands([267dd09](https://github.com/getarcaneapp/arcane/commit/267dd091a6311cab3a26ea35e07f116c01afd195) by @kmendell)
* incorrect return type for settings reponse([e3c323b](https://github.com/getarcaneapp/arcane/commit/e3c323b2c59b28e34096231300dd9dba32b29a6c) by @kmendell)

### Dependencies

* bump svelte from 5.46.0 to 5.46.1 ([#1229](https://github.com/getarcaneapp/arcane/pull/1229) by @dependabot[bot])
* bump @xterm/addon-fit from 0.10.0 to 0.11.0 ([#1232](https://github.com/getarcaneapp/arcane/pull/1232) by @dependabot[bot])



**Full Changelog**: https://github.com/getarcaneapp/arcane/compare/v1.11.0...v1.11.1

## v1.11.0

### Backend - New features

* gpu monitoring environment flags ([#1034](https://github.com/getarcaneapp/arcane/pull/1034) by @mrtimothyduong)
* new arcane info dialog([61ee1b8](https://github.com/getarcaneapp/arcane/commit/61ee1b80a6534f81d9246ce7c801e8374c9dc526) by @kmendell)
* add api key authentication ([#1046](https://github.com/getarcaneapp/arcane/pull/1046) by @kmendell)
* container update indicator directly in the table ([#1077](https://github.com/getarcaneapp/arcane/pull/1077) by @kmendell)
* inline start and stop functions for containers ([#1079](https://github.com/getarcaneapp/arcane/pull/1079) by @kmendell)
* show ports in project service overview panel([72298c4](https://github.com/getarcaneapp/arcane/commit/72298c431c23c4f3713f0fcce10c79b6c0ca12f8) by @kmendell)
* dynamic oidc redirect url based on url origin ([#1094](https://github.com/getarcaneapp/arcane/pull/1094) by @kmendell)
* sort containers connected to a network ([#1095](https://github.com/getarcaneapp/arcane/pull/1095) by @kmendell)
* ability to toggle between the classic project and tree view ui ([#1120](https://github.com/getarcaneapp/arcane/pull/1120) by @kmendell)
* auto pairing and new agent creation flow ([#1114](https://github.com/getarcaneapp/arcane/pull/1114) by @kmendell)
* new environment switcher dialog ([#1126](https://github.com/getarcaneapp/arcane/pull/1126) by @cabaucom376)
* add ip address column and cell to container table ([#1143](https://github.com/getarcaneapp/arcane/pull/1143) by @NeoCortex3)
* only send notifications when new updates arrive ([#1157](https://github.com/getarcaneapp/arcane/pull/1157) by @kmendell)
* unified tooltip/popover component ([#1125](https://github.com/getarcaneapp/arcane/pull/1125) by @cabaucom376)
* show agent version in details overview([8377fac](https://github.com/getarcaneapp/arcane/commit/8377fac80e179210bd53bb6b819911194ecde165) by @kmendell)
* environment settings now are edited on the agent details page ([#1206](https://github.com/getarcaneapp/arcane/pull/1206) by @kmendell)

### CLI - New features

* initial arcane-cli release ([#1150](https://github.com/getarcaneapp/arcane/pull/1150) by @kmendell)

### Backend - Bug fixes

* do not run environment health job on agent([123a621](https://github.com/getarcaneapp/arcane/commit/123a621a64d2ae22db76ec2fb0d08871e61a449b) by @kmendell)
* settings not allowing saving, and not updating immediately([86731e6](https://github.com/getarcaneapp/arcane/commit/86731e673bccec29442f875ff635594dc0b911eb) by @kmendell)
* add redirect from /auth/oidc/callback to /oidc/callback([ef621bf](https://github.com/getarcaneapp/arcane/commit/ef621bf2b95916f0ddde1f107643835cbe88ab0b) by @kmendell)
* missing tooltip providers causing lockups on mobile([8240e45](https://github.com/getarcaneapp/arcane/commit/8240e45bb35b7f835c885cf011f4535aa2192d5d) by @kmendell)
* pagination controls missing when containers grouped by project([74c0be1](https://github.com/getarcaneapp/arcane/commit/74c0be1cd3f295c6bc242296e1af2460cdd16727) by @kmendell)
* projects not being deleted on remote agents ([#1047](https://github.com/getarcaneapp/arcane/pull/1047) by @kmendell)
* update api url to APP_URL for local environment on each startup([96494c9](https://github.com/getarcaneapp/arcane/commit/96494c916135c54d89c1aa5d1cfbddc8b38e4a81) by @kmendell)
* geist font not being applied([43fc48b](https://github.com/getarcaneapp/arcane/commit/43fc48bb183d4cafdc67542ff3b8511b3ae5f977) by @kmendell)
* data race on initial loading of settings/users([80a9bef](https://github.com/getarcaneapp/arcane/commit/80a9bef0da4c1cc7c10ff178f5676fe35c3f268b) by @kmendell)
* bump github.com/quic-go/quic-go to 0.57.0 ([#1117](https://github.com/getarcaneapp/arcane/pull/1117) by @dependabot[bot])
* text selection color not able to be seen for terminal in light mode([a7bc358](https://github.com/getarcaneapp/arcane/commit/a7bc358c381d8f7e7ffe8c8f220209ff2e0a0633) by @kmendell)
* unsafe state mutation for notification test button([4461315](https://github.com/getarcaneapp/arcane/commit/44613150bf6479c231659c3eb05613ac2fdcccb5) by @kmendell)
* locale picker not selecting language ([#1148](https://github.com/getarcaneapp/arcane/pull/1148) by @cabaucom376)
* status badge text overflow([b598d7d](https://github.com/getarcaneapp/arcane/commit/b598d7da34979f1cc7d7f266b423fc9f83ed40c1) by @kmendell)
* oidc merge accounts failing to lookup emails ([#1154](https://github.com/getarcaneapp/arcane/pull/1154) by @kmendell)
* only save apprise settings if they changed([aba7525](https://github.com/getarcaneapp/arcane/commit/aba7525d2681aac3eebdb3c957020a1195e6786e) by @kmendell)
* show all network attributes from docker api ([#1171](https://github.com/getarcaneapp/arcane/pull/1171) by @kmendell)
* glass not being applying on actions popover([9d64fd7](https://github.com/getarcaneapp/arcane/commit/9d64fd75af63936bddc6f78cd2ed3eb7f80fbf06) by @kmendell)
* image pull progress not streaming from remote environments ([#1175](https://github.com/getarcaneapp/arcane/pull/1175) by @kmendell)
* improve Monaco IntelliSense based on compose spec ([#1190](https://github.com/getarcaneapp/arcane/pull/1190) by @kmendell)
* padding on bottom of other resource layouts([8a9960a](https://github.com/getarcaneapp/arcane/commit/8a9960a2bf0409429154fdcd0a54708739242a33) by @kmendell)
* settings state only allows one setting to be updated([56ad326](https://github.com/getarcaneapp/arcane/commit/56ad326a3c7c1914a2b3dd551dafab6f7a41cf85) by @kmendell)
* auto updater issues with certain network modes ([#1205](https://github.com/getarcaneapp/arcane/pull/1205) by @kmendell)
* mobile layout and performence issues([9816512](https://github.com/getarcaneapp/arcane/commit/981651263da009d8a2d6556448d107aa66367dbe) by @kmendell)
* image update available notifications now being sent([9500c9d](https://github.com/getarcaneapp/arcane/commit/9500c9d5ffa2cbd5d529133bad2792fb1c60187a) by @kmendell)

### CLI - Bug fixes

* move main.go to module root([024048b](https://github.com/getarcaneapp/arcane/commit/024048bda8de461a258032065da3da680c897cb6) by @kmendell)

### Backend - Performance improvements

* add missing database indexes to improve slow sql queries ([#1169](https://github.com/getarcaneapp/arcane/pull/1169) by @kmendell)

### Dependencies

* update dependabot configuration to weekly schedule and remove groups([a7f053a](https://github.com/getarcaneapp/arcane/commit/a7f053acaaa601f4b714237f1c0484471d6a912f) by @kmendell)
* bump react from 19.2.0 to 19.2.1 ([#1056](https://github.com/getarcaneapp/arcane/pull/1056) by @dependabot[bot])
* bump react-dom from 19.2.0 to 19.2.1 ([#1055](https://github.com/getarcaneapp/arcane/pull/1055) by @dependabot[bot])
* bump prettier from 3.7.3 to 3.7.4 ([#1057](https://github.com/getarcaneapp/arcane/pull/1057) by @dependabot[bot])
* bump peter-evans/find-comment from 3 to 4 ([#1084](https://github.com/getarcaneapp/arcane/pull/1084) by @dependabot[bot])
* bump peter-evans/create-or-update-comment from 4 to 5 ([#1083](https://github.com/getarcaneapp/arcane/pull/1083) by @dependabot[bot])
* bump github.com/spf13/cobra from 1.10.1 to 1.10.2 in /backend ([#1085](https://github.com/getarcaneapp/arcane/pull/1085) by @dependabot[bot])
* bump runed to 0.37.0 ([#1091](https://github.com/getarcaneapp/arcane/pull/1091) by @kmendell)
* bump github.com/go-co-op/gocron/v2 from 2.18.2 to 2.19.0 in /backend ([#1129](https://github.com/getarcaneapp/arcane/pull/1129) by @dependabot[bot])
* bump golang.org/x/text from 0.31.0 to 0.32.0 in /backend ([#1132](https://github.com/getarcaneapp/arcane/pull/1132) by @dependabot[bot])
* bump golang.org/x/oauth2 from 0.33.0 to 0.34.0 in /backend ([#1133](https://github.com/getarcaneapp/arcane/pull/1133) by @dependabot[bot])
* bump actions/cache from 4 to 5 ([#1127](https://github.com/getarcaneapp/arcane/pull/1127) by @dependabot[bot])
* bump golang.org/x/crypto from 0.45.0 to 0.46.0 in /backend ([#1130](https://github.com/getarcaneapp/arcane/pull/1130) by @dependabot[bot])
* bump stefanzweifel/git-auto-commit-action from 5 to 7 ([#1145](https://github.com/getarcaneapp/arcane/pull/1145) by @dependabot[bot])
* bump actions/download-artifact from 6 to 7 ([#1146](https://github.com/getarcaneapp/arcane/pull/1146) by @dependabot[bot])
* bump actions/upload-artifact from 5 to 6 ([#1144](https://github.com/getarcaneapp/arcane/pull/1144) by @dependabot[bot])
* bump react-dom from 19.2.1 to 19.2.3 ([#1137](https://github.com/getarcaneapp/arcane/pull/1137) by @dependabot[bot])
* bump pnpm to version 10.26.0([872f23a](https://github.com/getarcaneapp/arcane/commit/872f23af4037f4e0e2e2cc4cfb9513c75cf32a51) by @kmendell)
* bump github.com/goccy/go-yaml from 1.19.0 to 1.19.1 in /backend ([#1188](https://github.com/getarcaneapp/arcane/pull/1188) by @dependabot[bot])
* bump prettier-plugin-svelte from 3.4.0 to 3.4.1 ([#1186](https://github.com/getarcaneapp/arcane/pull/1186) by @dependabot[bot])
* bump github.com/docker/compose/v5 from 5.0.0 to 5.0.1 in /backend ([#1189](https://github.com/getarcaneapp/arcane/pull/1189) by @dependabot[bot])
* bump next in email templates, remove lezer/common override([d20cb3e](https://github.com/getarcaneapp/arcane/commit/d20cb3e3d8fdb76bf9e44a2ff77bda48c9facf14) by @kmendell)
* bump @react-email/components from 1.0.1 to 1.0.2 ([#1197](https://github.com/getarcaneapp/arcane/pull/1197) by @dependabot[bot])

### Other

* use go workspaces and svelte layout groups ([#1049](https://github.com/getarcaneapp/arcane/pull/1049) by @kmendell)
* use crowdin github action([0a85fef](https://github.com/getarcaneapp/arcane/commit/0a85fef5fdf53bc1bea935ba2e95bcf47b34e313) by @kmendell)
* revert crowdin github action([f71af94](https://github.com/getarcaneapp/arcane/commit/f71af94300b1bc7f7522fa37769bcf0e741da3b8) by @kmendell)
* use huma to generate open api spec on top of gin ([#1068](https://github.com/getarcaneapp/arcane/pull/1068) by @kmendell)
* serve application fonts from the backend embedded fs ([#1106](https://github.com/getarcaneapp/arcane/pull/1106) by @kmendell)
* ui layout improvements and polish ([#1102](https://github.com/getarcaneapp/arcane/pull/1102) by @kmendell)
* fix settings sidebar styling in non-glass mode([21e2adb](https://github.com/getarcaneapp/arcane/commit/21e2adb99620a0dde4eaa4ae60e2d8b4343ca0a9) by @kmendell)
* add cli binaries to release([851d08c](https://github.com/getarcaneapp/arcane/commit/851d08cb99bca45787bfcb817204072514469103) by @kmendell)
* remove deprecated onboarding types and logic ([#1176](https://github.com/getarcaneapp/arcane/pull/1176) by @kmendell)
* switch from code-mirror to monaco editor ([#1123](https://github.com/getarcaneapp/arcane/pull/1123) by @kmendell)
* ui layout and input tweaks and cleanup ([#1194](https://github.com/getarcaneapp/arcane/pull/1194) by @kmendell)
* split arcane table into multiple files ([#1201](https://github.com/getarcaneapp/arcane/pull/1201) by @kmendell)



**Full Changelog**: https://github.com/getarcaneapp/arcane/compare/v1.10.1...v1.11.0

## v1.10.1

### Bug fixes

* Pass project object to StartOptions ([#1010](https://github.com/getarcaneapp/arcane/pull/1010) by @chadweimer)
* dashboard memory stats not restrained to container limits([80ccc9b](https://github.com/getarcaneapp/arcane/commit/80ccc9b689afa7fa1e2ac87862970652772153cd) by @kmendell)
* exposed ports woroding rework will show published/exposed or a mix of the two([ebee116](https://github.com/getarcaneapp/arcane/commit/ebee116acc9dce85e772308228f2bbd8f11d4a63) by @kmendell)
* Tooltip.Provider not found causing env page to hang([3b3ff91](https://github.com/getarcaneapp/arcane/commit/3b3ff91744ed53ca7a9a6def2a1a030b3d9a1be2) by @kmendell)
* dashboard tables footer overlapping cells([305f46b](https://github.com/getarcaneapp/arcane/commit/305f46b46eea152c311f09ce2836ed4445993280) by @kmendell)
* bump validator to 13.15.22([9420308](https://github.com/getarcaneapp/arcane/commit/94203087e3f17887132076bd466ce2dad3bf6527) by @kmendell)
* arcane-table generic types([200f7ea](https://github.com/getarcaneapp/arcane/commit/200f7ea3fa88270b1e7bc480892f7a472b079af1) by @kmendell)
* use proper svelte reactivity logic to fix svelte-check warnings ([#1042](https://github.com/getarcaneapp/arcane/pull/1042) by @kmendell)
* set --primary-foreground as black in light mode([ff67565](https://github.com/getarcaneapp/arcane/commit/ff67565b1def18da4bb937d633911264ddbcce98) by @kmendell)

### Dependencies

* bump pnpm from 10.22.0 to 10.24.0([f840fb4](https://github.com/getarcaneapp/arcane/commit/f840fb435a48f540a092816e1c90087fc4664696) by @kmendell)
* bump node engine version requirement from >=24 to >=25([15a69ce](https://github.com/getarcaneapp/arcane/commit/15a69ced0ee4ec53e7cdadafac07cb5ae568c47d) by @kmendell)
* bump the dev-dependencies group across 1 directory with 3 updates ([#1028](https://github.com/getarcaneapp/arcane/pull/1028) by @dependabot[bot])
* upgrade to compose v5 and compose-go v2.10.0([33b6704](https://github.com/getarcaneapp/arcane/commit/33b670462df27adbfd177f434bc62e5e569df06a) by @kmendell)
* bump the backend-dependencies group across 1 directory with 3 updates ([#1037](https://github.com/getarcaneapp/arcane/pull/1037) by @dependabot[bot])
* remove unused frontend dependencies([316df73](https://github.com/getarcaneapp/arcane/commit/316df73f138743b7a18c94d4f7fd280a353da8d4) by @kmendell)

### Other

* update depot runner size([b2b3972](https://github.com/getarcaneapp/arcane/commit/b2b39724d382cd1bffa92cd197d1878f5fe190a3) by @kmendell)
* build next images sequentially([cde4d8b](https://github.com/getarcaneapp/arcane/commit/cde4d8bcfeb9da776e90ff659fd5389705cd7401) by @kmendell)
* build frontend for next images([af53ba7](https://github.com/getarcaneapp/arcane/commit/af53ba7229ceaf52df0507688eacd0d072578fb4) by @kmendell)
* build all binaries for next images([bc69eed](https://github.com/getarcaneapp/arcane/commit/bc69eeddd392d28b78c2946070796e189a507230) by @kmendell)
* build armv7 containers for next image([5d1f0d6](https://github.com/getarcaneapp/arcane/commit/5d1f0d6fef390e48821655cb7facdc9fcf664c20) by @kmendell)
* use sequential workflow and single binary command([826ecd3](https://github.com/getarcaneapp/arcane/commit/826ecd36624cbaba23b3c94daaa5d9c507f9b747) by @kmendell)
* simplify release workflow to single sequential job([f1e942a](https://github.com/getarcaneapp/arcane/commit/f1e942abc27c181c023df534d796d912c7bb6c12) by @kmendell)



**Full Changelog**: https://github.com/getarcaneapp/arcane/compare/v1.10.0...v1.10.1

## v1.10.0

### New features

- support for `include` compose directive ([eced698](https://github.com/getarcaneapp/arcane/commit/eced6980dc338966be2cb8aa413ef66897b1aebb) by @user00265)
- add version label to the login page([6d16da2](https://github.com/getarcaneapp/arcane/commit/6d16da243128ba293c6754606439fa91d2c1261a) by @kmendell)
- expand system info dialog to show more information ([#965](https://github.com/getarcaneapp/arcane/pull/965) by @kmendell)
- add support for auto-starting of logs ([#983](https://github.com/getarcaneapp/arcane/pull/983) by @Juoper)
- improved pull ui including layer progress ([#1002](https://github.com/getarcaneapp/arcane/pull/1002) by @kmendell)
- self update digest tracking and fixes ([#919](https://github.com/getarcaneapp/arcane/pull/919) by @cabaucom376)

### Bug fixes

- dashboard tables height cutting off rows([10f0d85](https://github.com/getarcaneapp/arcane/commit/10f0d85f6cbce6573188e205d1fd138d2ab9aa53) by @kmendell)
- pass registry credentials to environment ([d7a112a](https://github.com/getarcaneapp/arcane/commit/d7a112a08ae1b8cf39e8fbef68a96f6a117168e5) by @cabaucom376)
- use dynamic limit for dashboard tables on page refreshes([f0be4d2](https://github.com/getarcaneapp/arcane/commit/f0be4d2ccabdc71c8550db0d119421fcaf57be0a) by @kmendell)
- image name container table overflow([3675cca](https://github.com/getarcaneapp/arcane/commit/3675cca816da1b6af9bca36d9a5ad9aeb4c07ee4) by @kmendell)
- use correct PWD env variable ([#941](https://github.com/getarcaneapp/arcane/pull/941) by @kmendell)
- dashboard showing incorrect memory value([d49be99](https://github.com/getarcaneapp/arcane/commit/d49be99cd3fde6540a456bc5f51d101956598332) by @kmendell)
- redirect to root resource page when switching environments([223d5b4](https://github.com/getarcaneapp/arcane/commit/223d5b4625e9fb72e021dbac2c786e7810af9351) by @kmendell)
- notification settings not able to be saved due to incorrect key([4cd7ff5](https://github.com/getarcaneapp/arcane/commit/4cd7ff51c4b1dfbc298aca400c3c9d7895baf085) by @kmendell)
- dont sort global env alphabetically([cbca27b](https://github.com/getarcaneapp/arcane/commit/cbca27b51fb3be8f391d53d32df4538f81906ad2) by @kmendell)
- conflicting options: hostname and the network mode for auto update runs([f267dce](https://github.com/getarcaneapp/arcane/commit/f267dced92b438992b1be30d2e0e4f514a25057e) by @kmendell)
- oidc settings not saving after changing other security settings ([#1003](https://github.com/getarcaneapp/arcane/pull/1003) by @kmendell)

### Performance Improvements

- overall performance improvements and db transactions ([#951](https://github.com/getarcaneapp/arcane/pull/951) by @kmendell)

### Dependencies

- bump the prod-dependencies group with 4 updates ([0e3896c](https://github.com/getarcaneapp/arcane/commit/0e3896c56a7958cb3d4b172a95a62bbd34ddfdca) by @dependabot[bot])
- upgrade to node 25 for frontend builds ([85beddf](https://github.com/getarcaneapp/arcane/commit/85beddf0b07b52bb1672d902425ee936408e8153) by @kmendell)
- bump pnpm to version 10.22.0([394fd2e](https://github.com/getarcaneapp/arcane/commit/394fd2ee27c52c73d45ef49455b162a4ff910842) by @kmendell)
- bump golang.org/x/crypto from 0.44.0 to 0.45.0 in /backend in the backend-dependencies group ([#954](https://github.com/getarcaneapp/arcane/pull/954) by @dependabot[bot])
- bump actions/checkout from 5 to 6 ([#957](https://github.com/getarcaneapp/arcane/pull/957) by @dependabot[bot])
- bump github.com/coreos/go-oidc/v3 from 3.16.0 to 3.17.0 in /backend in the backend-dependencies group ([#973](https://github.com/getarcaneapp/arcane/pull/973) by @dependabot[bot])
- bump github.com/go-co-op/gocron/v2 from 2.18.0 to 2.18.1 in /backend in the backend-dependencies group ([#978](https://github.com/getarcaneapp/arcane/pull/978) by @dependabot[bot])
- bump github.com/go-co-op/gocron/v2 from 2.18.1 to 2.18.2 in /backend in the backend-dependencies group ([#992](https://github.com/getarcaneapp/arcane/pull/992) by @dependabot[bot])
- bump valibot to 1.2.0([040e741](https://github.com/getarcaneapp/arcane/commit/040e741222117daada34bfe5a8c7dd6f828c2950) by @kmendell)
- bump prettier from 3.6.2 to 3.7.1 in the dev-dependencies group ([#996](https://github.com/getarcaneapp/arcane/pull/996) by @dependabot[bot])

### Other

- update depot runners([7f5461e](https://github.com/getarcaneapp/arcane/commit/7f5461eaf7910de486b1d095da24a5bf058e8de7) by @kmendell)
- use constant type for config values([e13c3c7](https://github.com/getarcaneapp/arcane/commit/e13c3c7172a71ca116f97cc265135274d76bcc7a) by @kmendell)
- use constant for index.html([ba69bfd](https://github.com/getarcaneapp/arcane/commit/ba69bfd0e94c5182dfaf47ebc73f89a8882b3d81) by @kmendell)
- use constant errors to reduce duplication ([#964](https://github.com/getarcaneapp/arcane/pull/964) by @kmendell)
- use Debian Trixie for container builds/runtime ([#975](https://github.com/getarcaneapp/arcane/pull/975) by @kmendell)
- add sonarqube analysis([9cd9c49](https://github.com/getarcaneapp/arcane/commit/9cd9c49c00e6711c4888f27d9d356d0e1d7cf67d) by @kmendell)
- add coverage report to sonarqube([219cd8c](https://github.com/getarcaneapp/arcane/commit/219cd8cc3b698ffe082123696816e0a20011cba5) by @kmendell)
- rewrite go coverage report([08f5fee](https://github.com/getarcaneapp/arcane/commit/08f5fee7cb7a8f498917092653c679c24a4ea678) by @kmendell)
- update analytics job to use new domain([7e8c02f](https://github.com/getarcaneapp/arcane/commit/7e8c02f9f42459df8e9d5f8371d393390bd55b15) by @kmendell)
- remove sonarqube workflows([69eb206](https://github.com/getarcaneapp/arcane/commit/69eb20674c11d68936b34db3ebb91632473d3fc7) by @kmendell)
- update library used for image referencing ([#1007](https://github.com/getarcaneapp/arcane/pull/1007) by @kmendell)
- fix next release logic([d14a051](https://github.com/getarcaneapp/arcane/commit/d14a051be0904d79c7559570b4f4443ac28e6af0) by @kmendell)
- fix next release logic([88711f3](https://github.com/getarcaneapp/arcane/commit/88711f39013ce37c291a72b18794973c3b2fb074) by @kmendell)
- fix next release logic([ae40aec](https://github.com/getarcaneapp/arcane/commit/ae40aec44cbafefd0e50c4f17c5a84e59c32557c) by @kmendell)
- use field group components for specific pages([a01787b](https://github.com/getarcaneapp/arcane/commit/a01787b0f227585a1ba0b9161f17b66010c4dc59) by @kmendell)

**Full Changelog**: https://github.com/getarcaneapp/arcane/compare/v1.9.0...v1.10.0

## v1.9.0

### New features

- apprise notifications support ([#899](https://github.com/getarcaneapp/arcane/pull/899) by @kmendell)
- add gpu monitoring ([#799](https://github.com/getarcaneapp/arcane/pull/799) by @mrtimothyduong)
- support uploading local docker images ([#902](https://github.com/getarcaneapp/arcane/pull/902) by @kmendell)
- edit environments and show/edit local environment ([#915](https://github.com/getarcaneapp/arcane/pull/915) by @cabaucom376)
- add refresh button to project action buttons([4e5dcf4](https://github.com/getarcaneapp/arcane/commit/4e5dcf43f2b8cdbb3cdb7197d45b3d654730a441) by @kmendell)

### Bug fixes

- check for image locally before pulling when deploying project([7269ece](https://github.com/getarcaneapp/arcane/commit/7269ece30728cacf4fa61d29461121ab687de62a) by @kmendell)
- show popover when project name is missing for better user UX([f77044e](https://github.com/getarcaneapp/arcane/commit/f77044e60a71655f28330667150e7fcf8695ebab) by @kmendell)
- seperate prefefined networks from the rest in usage count([313bbbd](https://github.com/getarcaneapp/arcane/commit/313bbbd46a1d7501246769da921e6693ad8bfc8a) by @kmendell)
- environment switcher not showing added or removed items without refresh([34c1ddc](https://github.com/getarcaneapp/arcane/commit/34c1ddc87dc3af693b54804e9f7c2b997add5710) by @kmendell)
- environment data initialization on refreshes ([#925](https://github.com/getarcaneapp/arcane/pull/925) by @cabaucom376)
- missing tooltip provider ([#927](https://github.com/getarcaneapp/arcane/pull/927) by @cabaucom376)
- show correct stats based on cgroup restrictions ([#923](https://github.com/getarcaneapp/arcane/pull/923) by @kmendell)
- properly assign inUse status to volume return data([71623e3](https://github.com/getarcaneapp/arcane/commit/71623e33eadf0111beadb241a2cac8f36ab28739) by @kmendell)
- max depth control for project watcher ([#928](https://github.com/getarcaneapp/arcane/pull/928) by @kmendell)

### Dependencies

- bump the backend-dependencies group in /backend with 2 updates ([#882](https://github.com/getarcaneapp/arcane/pull/882) by @dependabot[bot])
- bump the prod-dependencies group with 3 updates ([#881](https://github.com/getarcaneapp/arcane/pull/881) by @dependabot[bot])
- bump pnpm to 10.21.0([fb63171](https://github.com/getarcaneapp/arcane/commit/fb6317125b687f3f368e3201052ac1206394a9dc) by @kmendell)
- bump cookie to 0.7.0([4a90b2b](https://github.com/getarcaneapp/arcane/commit/4a90b2b57e4d70101ce82f3c10324cde00aef2fe) by @kmendell)
- bump the prod-dependencies group with 4 updates ([#900](https://github.com/getarcaneapp/arcane/pull/900) by @dependabot[bot])
- bump the prod-dependencies group with 2 updates ([#920](https://github.com/getarcaneapp/arcane/pull/920) by @dependabot[bot])
- bump actions/download-artifact from 5 to 6 ([#929](https://github.com/getarcaneapp/arcane/pull/929) by @dependabot[bot])

### Other

- refactor next image workflow([f5c3fca](https://github.com/getarcaneapp/arcane/commit/f5c3fcaf2116d51f62349f28f72f68429a63ded0) by @kmendell)
- use upload-artifact v5([ef12e17](https://github.com/getarcaneapp/arcane/commit/ef12e17ff13fdd9b2d92a471cec1ac44667105e7) by @kmendell)
- use go cache path([48e4cc6](https://github.com/getarcaneapp/arcane/commit/48e4cc630149dfce5908db598051302e11b521fb) by @kmendell)
- workflow refactors and tweaks([d8f8d53](https://github.com/getarcaneapp/arcane/commit/d8f8d53816d5c83c2572da40fb01555debf927ae) by @kmendell)
- use github oidc for depot in release workflow([c2764db](https://github.com/getarcaneapp/arcane/commit/c2764db7564a40fc32fb6712bc138f1d14966b12) by @kmendell)

**Full Changelog**: https://github.com/getarcaneapp/arcane/compare/v1.8.1...v1.9.0

## v1.8.1

### Bug fixes

- null checks for tag arrays in template viewer([55c592e](https://github.com/getarcaneapp/arcane/commit/55c592e42250d5b4a97748caaa1423f495fde536) by @kmendell)
- template details page now loading when no env is found([b7203ce](https://github.com/getarcaneapp/arcane/commit/b7203ce2d71c588f4c0444ad51608c85341d3b64) by @kmendell)
- header showing background in non-glass ui mode([d4811f1](https://github.com/getarcaneapp/arcane/commit/d4811f14cd554d0368d6ec0b709300f4e2de4700) by @kmendell)
- individual image update checks not using credentials([64cd997](https://github.com/getarcaneapp/arcane/commit/64cd997573777cc49ad27c9ca271f243f5ed9a41) by @kmendell)
- normzalize project name on load([754b426](https://github.com/getarcaneapp/arcane/commit/754b4262e4f84e4c2dbd27f4e79373b9a118a090) by @kmendell)
- discord notifications unable to be saved due to zod validations([fae1c94](https://github.com/getarcaneapp/arcane/commit/fae1c94caa8fa64a496a30bc9bc981aea5cbd782) by @kmendell)
- clear notification values from database is disabled([b8d10c9](https://github.com/getarcaneapp/arcane/commit/b8d10c9cff05147f815a516702b6e827159bfb8a) by @kmendell)
- remove entrypoint to fix file permission issues ([#879](https://github.com/getarcaneapp/arcane/pull/879) by @kmendell)
- tooltip showing on env switcher in sidebar hover mode([757baf6](https://github.com/getarcaneapp/arcane/commit/757baf68b95704336f712a678364fea4b2bed62c) by @kmendell)

### Dependencies

- bump golangci/golangci-lint-action from 8 to 9 ([#872](https://github.com/getarcaneapp/arcane/pull/872) by @dependabot[bot])

**Full Changelog**: https://github.com/getarcaneapp/arcane/compare/v1.8.0...v1.8.1

## v1.8.0

### New features

- show image repo on untagged images ([#809](https://github.com/getarcaneapp/arcane/pull/809) by @kmendell)
- save compose files to local templates ([#834](https://github.com/getarcaneapp/arcane/pull/834) by @cabaucom376)
- merge oidc users with existing users toggle ([#860](https://github.com/getarcaneapp/arcane/pull/860) by @kmendell)

### Bug fixes

- pass version to docker file builds([c28abd4](https://github.com/getarcaneapp/arcane/commit/c28abd4798dfea7dbcb6ec9ecaef902ca785f117) by @kmendell)
- syntax highlighting not showing on code-editors([ab03008](https://github.com/getarcaneapp/arcane/commit/ab0300866f30231bb629c7158f6eec6d387e0375) by @kmendell)
- show ansi colors in log viewer([a6be63d](https://github.com/getarcaneapp/arcane/commit/a6be63d70d555a2c594390051cac0c697a4eb0df) by @kmendell)
- status badges not reactive to states([88fb669](https://github.com/getarcaneapp/arcane/commit/88fb669c5acc39752344e6ccf09e7dee4b52a491) by @kmendell)
- unable to create ipvlan or macvlan networks([5f4e9e0](https://github.com/getarcaneapp/arcane/commit/5f4e9e02eb0697789230c313e22e2c72492a8cb4) by @kmendell)
- unpinned sidebar overflow scrolling in non-expanded state ([#831](https://github.com/getarcaneapp/arcane/pull/831) by @cabaucom376)
- volumes not removed when selected on container removal dialog([bb928fe](https://github.com/getarcaneapp/arcane/commit/bb928fe72af2fadb912e7b4685667592f48101d6) by @kmendell)
- use proper derived logic for status baddge([e0d66b5](https://github.com/getarcaneapp/arcane/commit/e0d66b58bd45da4ad9e2a594bdee9dc0c8a41942) by @kmendell)

### Dependencies

- update module github.com/compose-spec/compose-go/v2 to v2.9.1 ([#810](https://github.com/getarcaneapp/arcane/pull/810) by @renovate[bot])
- update module github.com/docker/compose/v5 to v2.40.3 ([#811](https://github.com/getarcaneapp/arcane/pull/811) by @renovate[bot])
- update dependency svelte to v5.43.1 ([#814](https://github.com/getarcaneapp/arcane/pull/814) by @renovate[bot])
- update dependency eslint-plugin-svelte to v3.13.0 ([#813](https://github.com/getarcaneapp/arcane/pull/813) by @renovate[bot])
- update dependency svelte to v5.43.2 ([#816](https://github.com/getarcaneapp/arcane/pull/816) by @renovate[bot])
- update dependency @sveltejs/kit to v2.48.4 ([#819](https://github.com/getarcaneapp/arcane/pull/819) by @renovate[bot])
- update module github.com/shirou/gopsutil/v4 to v4.25.10 ([#827](https://github.com/getarcaneapp/arcane/pull/827) by @renovate[bot])
- update dependency @lucide/svelte to ^0.552.0 ([#822](https://github.com/getarcaneapp/arcane/pull/822) by @renovate[bot])
- update dependency bits-ui to v2.14.2 ([#826](https://github.com/getarcaneapp/arcane/pull/826) by @renovate[bot])
- update dependency globals to v16.5.0 ([#828](https://github.com/getarcaneapp/arcane/pull/828) by @renovate[bot])
- update eslint monorepo to v9.39.0 ([#825](https://github.com/getarcaneapp/arcane/pull/825) by @renovate[bot])
- update dependency isomorphic-dompurify to v2.31.0 ([#832](https://github.com/getarcaneapp/arcane/pull/832) by @renovate[bot])
- update module gorm.io/gorm to v1.31.1 ([#833](https://github.com/getarcaneapp/arcane/pull/833) by @renovate[bot])
- update dependency typescript-eslint to v8.46.3 ([#840](https://github.com/getarcaneapp/arcane/pull/840) by @renovate[bot])
- update dependency svelte to v5.43.3 ([#843](https://github.com/getarcaneapp/arcane/pull/843) by @renovate[bot])
- update dependency @codemirror/lint to v6.9.2 ([#838](https://github.com/getarcaneapp/arcane/pull/838) by @renovate[bot])
- update eslint monorepo to v9.39.1 ([#841](https://github.com/getarcaneapp/arcane/pull/841) by @renovate[bot])
- update dependency @types/node to v24.10.0 ([#836](https://github.com/getarcaneapp/arcane/pull/836) by @renovate[bot])
- update dependency axios to v1.13.2 ([#844](https://github.com/getarcaneapp/arcane/pull/844) by @renovate[bot])
- update dependency @uiw/codemirror-theme-github to v4.25.3 ([#846](https://github.com/getarcaneapp/arcane/pull/846) by @renovate[bot])
- update dependency @uiw/codemirror-themes to v4.25.3 ([#847](https://github.com/getarcaneapp/arcane/pull/847) by @renovate[bot])
- update dependency vite to v7.2.0 ([#849](https://github.com/getarcaneapp/arcane/pull/849) by @renovate[bot])
- update dependency sveltekit-superforms to v2.28.1 ([#848](https://github.com/getarcaneapp/arcane/pull/848) by @renovate[bot])
- update dependency svelte to v5.43.4 ([#858](https://github.com/getarcaneapp/arcane/pull/858) by @renovate[bot])
- bump github.com/containerd/containerd/v2 from 2.1.4 to 2.1.5 in /backend in the go_modules group across 1 directory ([#857](https://github.com/getarcaneapp/arcane/pull/857) by @dependabot[bot])
- update dependency vite to v7.2.1 ([#854](https://github.com/getarcaneapp/arcane/pull/854) by @renovate[bot])
- update module github.com/docker/docker to v28.5.2+incompatible ([#852](https://github.com/getarcaneapp/arcane/pull/852) by @renovate[bot])
- update module github.com/docker/cli to v28.5.2+incompatible ([#851](https://github.com/getarcaneapp/arcane/pull/851) by @renovate[bot])
- update module github.com/go-co-op/gocron/v2 to v2.18.0 ([#853](https://github.com/getarcaneapp/arcane/pull/853) by @renovate[bot])
- update tailwindcss monorepo to v4.1.17 ([#859](https://github.com/getarcaneapp/arcane/pull/859) by @renovate[bot])
- update dependency vite to v7.2.2 ([#864](https://github.com/getarcaneapp/arcane/pull/864) by @renovate[bot])
- update dependency @lucide/svelte to ^0.553.0 ([#865](https://github.com/getarcaneapp/arcane/pull/865) by @renovate[bot])
- update dependency react-email to v5 ([#868](https://github.com/getarcaneapp/arcane/pull/868) by @renovate[bot])
- update dependency @react-email/preview-server to v5 ([#867](https://github.com/getarcaneapp/arcane/pull/867) by @renovate[bot])
- update dependency @react-email/components to v1 ([#866](https://github.com/getarcaneapp/arcane/pull/866) by @renovate[bot])
- update dependency vite to v7.2.2 ([#869](https://github.com/getarcaneapp/arcane/pull/869) by @renovate[bot])

### Other

- responsive create container dialog ([#830](https://github.com/getarcaneapp/arcane/pull/830) by @cabaucom376)
- use depot builders and images([2bb159d](https://github.com/getarcaneapp/arcane/commit/2bb159d6996abcc223c73ccce9a39b1f42af1283) by @kmendell)
- fix typo in image name([2e49791](https://github.com/getarcaneapp/arcane/commit/2e49791c40ccc55b50d6736eab9dec98e4f97236) by @kmendell)
- build e2e tests image with depot([52aaac4](https://github.com/getarcaneapp/arcane/commit/52aaac459c6cb27f0cde0ea0c3649b306a6f86ef) by @kmendell)
- use depot token([6ad13a3](https://github.com/getarcaneapp/arcane/commit/6ad13a3dfa9f540650350afc1568e24f8c4f95c0) by @kmendell)
- remove double outputs([3a619d0](https://github.com/getarcaneapp/arcane/commit/3a619d0e24a85080a7dc3aa467d45c07a4a32404) by @kmendell)
- use depot registry([0cebb42](https://github.com/getarcaneapp/arcane/commit/0cebb42cd164b6703d7e99df77b0d0c3d5ae1aa3) by @kmendell)
- use depot token for all builds([65601d5](https://github.com/getarcaneapp/arcane/commit/65601d55e294710625d184a8adae6acc5024b1a0) by @kmendell)
- use go cache from depot([e83c717](https://github.com/getarcaneapp/arcane/commit/e83c717a96cc4e346d2e6edb67b11a922db0cb38) by @kmendell)
- revert depot usage for some workflows([fd369d1](https://github.com/getarcaneapp/arcane/commit/fd369d1d39b3e3e98c3608e36e23ce58d2ba634a) by @kmendell)
- extended dashboard tables ([#871](https://github.com/getarcaneapp/arcane/pull/871) by @kmendell)

**Full Changelog**: https://github.com/getarcaneapp/arcane/compare/v1.7.2...v1.8.0

## v1.7.2

### Bug fixes

- syntax highlighting not showing on code-editors([027155e](https://github.com/ofkm/arcane/commit/027155e092d0b91ff6c9b52d77149456deac9f72) by @Kyle Mendell)
- pass version to docker file builds([8a136cd](https://github.com/ofkm/arcane/commit/8a136cd51e52f4721452e1a32d4a17523a22de1c) by @Kyle Mendell)

**Full Changelog**: https://github.com/ofkm/arcane/compare/v1.7.1...v1.7.2

## v1.7.1

### Bug fixes

- mobile overscroll issues([fa0a7d2](https://github.com/ofkm/arcane/commit/fa0a7d254cf2339a55ba76303b585d43b93dda1c) by @kmendell)
- use correct background colors for components when glass is disabled([615c21f](https://github.com/ofkm/arcane/commit/615c21f13244a95c00bdb22e62209a8876b16a89) by @kmendell)
- event details dialog header padding([55dca66](https://github.com/ofkm/arcane/commit/55dca6690e811e06cbb9e4cd159fefb4847903fc) by @kmendell)
- cpu count not showing under cpu meter([30cab56](https://github.com/ofkm/arcane/commit/30cab5657f79cccf9e07fb4394ced5db9e4d8f31) by @kmendell)
- settings state not persisted acorss settings pages([46bbe93](https://github.com/ofkm/arcane/commit/46bbe93c862d8f7014e2303f8280e33a293dae10) by @kmendell)
- notification settings does not show on the settings overview page([6354f9c](https://github.com/ofkm/arcane/commit/6354f9c3cf73a7fe96815b39634d3ad54feecfd2) by @kmendell)
- properly log error events to event table([caef210](https://github.com/ofkm/arcane/commit/caef21052fb92be51588da7eff5d301612f2e59a) by @kmendell)

### Other

- bump svelte from 5.42.3 to 5.43.0 in the prod-dependencies group ([#796](https://github.com/ofkm/arcane/pull/796) by @dependabot[bot])
- bump @types/node from 24.9.1 to 24.9.2 in the dev-dependencies group ([#797](https://github.com/ofkm/arcane/pull/797) by @dependabot[bot])
- Configure Renovate ([#803](https://github.com/ofkm/arcane/pull/803) by @renovate[bot])
- remove dependabot config([019f3d6](https://github.com/ofkm/arcane/commit/019f3d6ee8279cf3173de51a048121721e45616e) by @kmendell)
- update renovate config to include semantic commit type for chores([a2f24ce](https://github.com/ofkm/arcane/commit/a2f24ce4f1c836a6a89fec0525fa9fff8d3674d1) by @kmendell)
- disable renovate dashboard([dc79404](https://github.com/ofkm/arcane/commit/dc7940466ac2a8672797434208b9b8d255431757) by @kmendell)
- update dependency @sveltejs/kit to v2.48.3 ([#802](https://github.com/ofkm/arcane/pull/802) by @renovate[bot])
- update dependency @eslint/compat to v1.4.1 ([#801](https://github.com/ofkm/arcane/pull/801) by @renovate[bot])
- upgrade github.com/shirou/gopsutil to v4([57fa42c](https://github.com/ofkm/arcane/commit/57fa42c4f18c1eb1854d46b939694e2ec4561599) by @kmendell)
- layout and design of docker info dialog([3004447](https://github.com/ofkm/arcane/commit/300444703d6770f5372a28c2cec852ddaeeeca7d) by @kmendell)
- use SettingsForm hook for managing settings logic and state([e09a808](https://github.com/ofkm/arcane/commit/e09a808d09f7f1593969be11a43707224d6c7398) by @kmendell)
- fix backend linter([d36f6ad](https://github.com/ofkm/arcane/commit/d36f6ad566d504ad03126a52583c1493bff8fe54) by @kmendell)

**Full Changelog**: https://github.com/ofkm/arcane/compare/v1.7.0...v1.7.1

## v1.7.0

### New features

- arcane cli `generate secret` command ([#760](https://github.com/ofkm/arcane/pull/760) by @kmendell)
- notification system for container and image updates ([#730](https://github.com/ofkm/arcane/pull/730) by @Copilot)
- add hostname to dashboard([492222d](https://github.com/ofkm/arcane/commit/492222d26872fd51879760345bdfa4bfe1c66e39) by @kmendell)
- arcane self-updater ([#744](https://github.com/ofkm/arcane/pull/744) by @kmendell)

### Bug fixes

- text inputs not using correct string or number type for input boxes([215d8ed](https://github.com/ofkm/arcane/commit/215d8ed6e3847f7405607118f0ac986f089e5ae2) by @kmendell)
- project env interpolation not parsing env files in correct order ([#751](https://github.com/ofkm/arcane/pull/751) by @kmendell)
- load project services concurrently ([#758](https://github.com/ofkm/arcane/pull/758) by @kmendell)
- only log entrypoint when running the base arcane command([e95093f](https://github.com/ofkm/arcane/commit/e95093f2ad90b878d43ffb91b8d7a12eaf654f0c) by @kmendell)
- refresh environment on all pages([44501d4](https://github.com/ofkm/arcane/commit/44501d492f88f5b8e6f938e4844b3568406012e9) by @kmendell)
- properly persist local vs environment settings ([#775](https://github.com/ofkm/arcane/pull/775) by @kmendell)
- remove default route from customize overview ([#776](https://github.com/ofkm/arcane/pull/776) by @cabaucom376)
- responsive dialog overflow ([#786](https://github.com/ofkm/arcane/pull/786) by @cabaucom376)
- scroll-to-hide by mode, fix detachment ([#781](https://github.com/ofkm/arcane/pull/781) by @cabaucom376)
- containers unable to be sorted by name([f1905ed](https://github.com/ofkm/arcane/commit/f1905ed1b95caef0f7637de8058aeae338a83ba8) by @kmendell)
- stat card columns([ac05b46](https://github.com/ofkm/arcane/commit/ac05b463c7ed7537d1ebcf2f08aca2c87071654c) by @kmendell)
- date locale cant be loaded from date-fns if locale is en([947655c](https://github.com/ofkm/arcane/commit/947655c30dd9be29c81a42a0ccce35d1a6fdb858) by @kmendell)
- popovers fully transparent without glassmorphism ([#793](https://github.com/ofkm/arcane/pull/793) by @cabaucom376)
- encryption key validation errors([b3c7cb5](https://github.com/ofkm/arcane/commit/b3c7cb5bd64c6a5d49a0950ae7bde01f42910f4d) by @kmendell)
- sessions not using refresh tokens ([#795](https://github.com/ofkm/arcane/pull/795) by @kmendell)

### Other

- bump the dev-dependencies group with 3 updates ([#743](https://github.com/ofkm/arcane/pull/743) by @dependabot[bot])
- bump github.com/docker/compose/v5 from 2.40.0 to 2.40.1 in /backend in the backend-dependencies group ([#745](https://github.com/ofkm/arcane/pull/745) by @dependabot[bot])
- bump the prod-dependencies group with 6 updates ([#742](https://github.com/ofkm/arcane/pull/742) by @dependabot[bot])
- use ellipsis instead of three dots in source strings ([#748](https://github.com/ofkm/arcane/pull/748) by @blfpd)
- use ellipsis instead of three dots in translated strings ([#749](https://github.com/ofkm/arcane/pull/749) by @blfpd)
- add greptile.json configuration file([7e3c645](https://github.com/ofkm/arcane/commit/7e3c645e0cc8c75d245338840a343cf1664088b9) by @kmendell)
- bump vite from 7.1.10 to 7.1.11 in the npm_and_yarn group across 1 directory ([#752](https://github.com/ofkm/arcane/pull/752) by @dependabot[bot])
- move settings/customize search to backend ([#739](https://github.com/ofkm/arcane/pull/739) by @kmendell)
- bump @types/node from 24.8.1 to 24.9.1 in the dev-dependencies group ([#754](https://github.com/ofkm/arcane/pull/754) by @dependabot[bot])
- bump the prod-dependencies group with 4 updates ([#753](https://github.com/ofkm/arcane/pull/753) by @dependabot[bot])
- fix linter about complexity([bc99f5a](https://github.com/ofkm/arcane/commit/bc99f5af5d03ddf01854140d335d309b005322e3) by @kmendell)
- remove onboarding screens with simple change password dialog ([#759](https://github.com/ofkm/arcane/pull/759) by @kmendell)
- bump isomorphic-dompurify from 2.29.0 to 2.30.0 in the prod-dependencies group ([#761](https://github.com/ofkm/arcane/pull/761) by @dependabot[bot])
- bump pnpm to 10.19.0([7046e7e](https://github.com/ofkm/arcane/commit/7046e7e1f3e7ee79f21095c41bc8b25251a1e2e0) by @kmendell)
- show correct version on non-release builds([d0b6b35](https://github.com/ofkm/arcane/commit/d0b6b354de711e6abc392c7e5eb2ac4238bef744) by @kmendell)
- pass version arguments to next build([be7222a](https://github.com/ofkm/arcane/commit/be7222ae7eb707c306be9ae593071a7ca81c52e4) by @kmendell)
- bump frontend and tests pnpm version to 10.19.0([a441293](https://github.com/ofkm/arcane/commit/a441293ad308d9c06dbbb7347e5db146f6762755) by @kmendell)
- tweak glass-ui styling([f6edd50](https://github.com/ofkm/arcane/commit/f6edd5025e9522e7f0978c26bf8fc37521073c0b) by @kmendell)
- bump vite from 7.1.11 to 7.1.12 in the dev-dependencies group ([#769](https://github.com/ofkm/arcane/pull/769) by @dependabot[bot])
- bump the prod-dependencies group across 1 directory with 9 updates ([#774](https://github.com/ofkm/arcane/pull/774) by @dependabot[bot])
- tweak glass ui on sheets and dialogs([051fee6](https://github.com/ofkm/arcane/commit/051fee6d18d729bd52b49435fc6a0db1fec83516) by @kmendell)
- update backend deps([7de538c](https://github.com/ofkm/arcane/commit/7de538c550da99aaa6bc54e3f2536afa79ebdff5) by @kmendell)
- use env interpolation in project tests([7ba22ce](https://github.com/ofkm/arcane/commit/7ba22ceaef0b6c80a76a688185114696bbb02a37) by @kmendell)
- use correct project logs heading selector([2742223](https://github.com/ofkm/arcane/commit/2742223e8b191c032543bd610ccafc7227794451) by @kmendell)
- use correct project logs button selectors([dbecb46](https://github.com/ofkm/arcane/commit/dbecb460bffed17089d3ae527b9e989b4549bc58) by @kmendell)
- use drawers on mobile and dialogs on desktop([89beadc](https://github.com/ofkm/arcane/commit/89beadc5dcdbcc03ccd4426e494803179a760a95) by @kmendell)
- better align glass ui in production builds([ed9b171](https://github.com/ofkm/arcane/commit/ed9b1715542fd145c7f9618fe77eff8cb1a00c24) by @kmendell)
- login screen styling ([#780](https://github.com/ofkm/arcane/pull/780) by @cabaucom376)
- bump the prod-dependencies group with 4 updates ([#784](https://github.com/ofkm/arcane/pull/784) by @dependabot[bot])
- bump actions/upload-artifact from 4 to 5 ([#782](https://github.com/ofkm/arcane/pull/782) by @dependabot[bot])
- bump actions/download-artifact from 5 to 6 ([#783](https://github.com/ofkm/arcane/pull/783) by @dependabot[bot])
- update dialog overflow ([#788](https://github.com/ofkm/arcane/pull/788) by @cabaucom376)
- use accent color for card header ([#789](https://github.com/ofkm/arcane/pull/789) by @cabaucom376)
- table styling ([#787](https://github.com/ofkm/arcane/pull/787) by @cabaucom376)
- run formatter([a89581e](https://github.com/ofkm/arcane/commit/a89581e2a3229c29bc6cd9628edbc3191ba0795c) by @kmendell)
- restore solid backgrounds for non-glass ui([38f919b](https://github.com/ofkm/arcane/commit/38f919b3e6fbe834ee0108a33bc94e0a068405e1) by @kmendell)
- use dropdown card component for group by project([b26f873](https://github.com/ofkm/arcane/commit/b26f87347697187fee4d8a717d10e17061a9f3bb) by @kmendell)
- use new ubuntu-slim runner for smaller workflows([51213a4](https://github.com/ofkm/arcane/commit/51213a40ed798a7b6f9124a75a6027e64d35c7ef) by @kmendell)
- revert ubuntu-slim workflows([6ead191](https://github.com/ofkm/arcane/commit/6ead191a36e5e38f06b6db8d87c0482d7b5d8a28) by @kmendell)
- bump validator.js to 13.15.20([db216dd](https://github.com/ofkm/arcane/commit/db216dd5383e2f53f8d3453868996a234d7a6e5d) by @kmendell)
- bump pnpm to 10.20.0([5241be7](https://github.com/ofkm/arcane/commit/5241be7aeb2aae2202d0e00a371cfe0d6426d3a0) by @kmendell)
- table cell newline wrapping fix ([#792](https://github.com/ofkm/arcane/pull/792) by @cabaucom376)
- bump the prod-dependencies group across 1 directory with 6 updates ([#794](https://github.com/ofkm/arcane/pull/794) by @dependabot[bot])
- bump @types/dockerode from 3.3.44 to 3.3.45 in the dev-dependencies group ([#785](https://github.com/ofkm/arcane/pull/785) by @dependabot[bot])
- change upgrader image tag to 'latest'([882edae](https://github.com/ofkm/arcane/commit/882edae20675cb23f22e33fd8e01b9219592b06b) by @kmendell)

**Full Changelog**: https://github.com/ofkm/arcane/compare/v1.6.0...v1.7.0

## v1.6.0

### New features

- redeisgned templates pages ([#648](https://github.com/ofkm/arcane/pull/648) by @cabaucom376)
- show project status reason ([#708](https://github.com/ofkm/arcane/pull/708) by @kmendell)
- configurable hover and pinned states for sidebar ([#720](https://github.com/ofkm/arcane/pull/720) by @kmendell)
- add glass effect ui preview (disabled by default) ([#733](https://github.com/ofkm/arcane/pull/733) by @kmendell)

### Bug fixes

- mobile navigation glitches and scrolling issues ([#709](https://github.com/ofkm/arcane/pull/709) by @kmendell)
- show users full name instead of username in mobile user card([0086f31](https://github.com/ofkm/arcane/commit/0086f314fecacd4f37794992f42d299b85b46502) by @kmendell)
- display toast at the top on mobile and tablet devices([80decef](https://github.com/ofkm/arcane/commit/80decefdf27621a8373a0b009da1cbed0736bc8b) by @kmendell)
- save button showing behind mobile nav bar([574e22c](https://github.com/ofkm/arcane/commit/574e22c6ec0ab2c6f6eb2df4cb8d2fff34df6ba4) by @kmendell)
- environment hostname text showing under select input on mobile sheet([ac0f1fa](https://github.com/ofkm/arcane/commit/ac0f1fa671a045aecd4792825d44b6af7eda16ff) by @kmendell)
- card headers not reactive to mode-watchers theme([6aad161](https://github.com/ofkm/arcane/commit/6aad16113316fbcea28c14785074f620fe363ae9) by @kmendell)
- display full image sha256 on image details page([2f0bab5](https://github.com/ofkm/arcane/commit/2f0bab53a9089f23ed00b4975109231e1b825f3c) by @kmendell)
- show customization sub routes in mobile navigation([07713ce](https://github.com/ofkm/arcane/commit/07713ced938195def6852ac8dc35d8f9d1b5f009) by @kmendell)
- mobile nav sheet not working on small desktop screens([14bd338](https://github.com/ofkm/arcane/commit/14bd338107b2e8136599599116fe88cd0fee015d) by @kmendell)
- velocity to open nav sheet in mobile view([3d436a1](https://github.com/ofkm/arcane/commit/3d436a13ff78f48d9922affdfd4d7e9d0e6deda6) by @kmendell)
- sub nav items now showing in hover mode([c60e954](https://github.com/ofkm/arcane/commit/c60e954a7ffd92319bd028111fccc3bd035c34d9) by @kmendell)
- include version in jwt for auth sessions after updaing the server ([#724](https://github.com/ofkm/arcane/pull/724) by @kmendell)
- update file permissions to 0644 for project and templatye file creations([ca66f44](https://github.com/ofkm/arcane/commit/ca66f44df33f8f5520776371b3c2303f4b9ff42e) by @kmendell)
- handle Docker socket GID conflict in entrypoint ([#735](https://github.com/ofkm/arcane/pull/735) by @jae-jae)
- hidden table coumns data still showing even if the column is hidden([aced2a6](https://github.com/ofkm/arcane/commit/aced2a6eefaaba89525b8cbfd180e826fac92891) by @kmendell)
- update icon import ([#737](https://github.com/ofkm/arcane/pull/737) by @cabaucom376)
- z-index of search icons on settings and customize pages([87af81c](https://github.com/ofkm/arcane/commit/87af81c68475c84acdfe46c62ae97e34370320a5) by @kmendell)
- sidebar items not expanding in non hover mode([515d442](https://github.com/ofkm/arcane/commit/515d4420ff3ef5e231b0175b9f388165a7929295) by @kmendell)

### Other

- add Deutsch files([31dfa46](https://github.com/ofkm/arcane/commit/31dfa46d96e254f3bc72d48e7b56d24da4fc20c4) by @kmendell)
- bump github.com/go-co-op/gocron/v2 from 2.16.6 to 2.17.0 in /backend in the backend-dependencies group ([#701](https://github.com/ofkm/arcane/pull/701) by @dependabot[bot])
- bump the prod-dependencies group with 3 updates ([#699](https://github.com/ofkm/arcane/pull/699) by @dependabot[bot])
- bump @types/node from 24.7.1 to 24.7.2 in the dev-dependencies group ([#700](https://github.com/ofkm/arcane/pull/700) by @dependabot[bot])
- restructure scripts directory([9cd3333](https://github.com/ofkm/arcane/commit/9cd3333f2744ef7f391a31f7049e8980e4ff0617) by @kmendell)
- service worker caching tweaks([86057f1](https://github.com/ofkm/arcane/commit/86057f1c96976e8237122c2734c5451a360b9b4e) by @kmendell)
- update runner tags([c04af97](https://github.com/ofkm/arcane/commit/c04af97593922143035586f499df9524d00d1a9b) by @kmendell)
- upgrade deps([614130a](https://github.com/ofkm/arcane/commit/614130aace9470669acc4c4af5da9f6a0099a225) by @kmendell)
- remove docker build cloud from ci/cd([3063dc1](https://github.com/ofkm/arcane/commit/3063dc161fa9a72f31cdbad2a7064cf0cdeb3b32) by @kmendell)
- pin runed to 0.34.0([2946ad7](https://github.com/ofkm/arcane/commit/2946ad79d0a40596be020cef2c4b431778ebb982) by @kmendell)
- fix linter([d6290b6](https://github.com/ofkm/arcane/commit/d6290b67823c29713ab3b2a6674f5c202b98f41c) by @kmendell)
- fix after last merge([4a73728](https://github.com/ofkm/arcane/commit/4a73728f12eb8410f3547a93b6cc58491fb4f275) by @kmendell)
- bump the dev-dependencies group with 2 updates ([#716](https://github.com/ofkm/arcane/pull/716) by @dependabot[bot])
- bump the prod-dependencies group across 1 directory with 3 updates ([#717](https://github.com/ofkm/arcane/pull/717) by @dependabot[bot])
- pin setup-node to latest commit hash of v5([4851729](https://github.com/ofkm/arcane/commit/48517296c3650059a0e5d705e7a0235541b5f652) by @kmendell)
- bump sveltekit-superforms from 2.27.3 to 2.27.4 in the npm_and_yarn group across 1 directory ([#719](https://github.com/ofkm/arcane/pull/719) by @dependabot[bot])
- consolidate build next workflows using matrix([5e4229b](https://github.com/ofkm/arcane/commit/5e4229b39bce53e1cb452505c4d80e7eb3fa8270) by @kmendell)
- build next images one after another([d06ec48](https://github.com/ofkm/arcane/commit/d06ec48070012393f59f547b1d14d28008f334b0) by @kmendell)
- use ubuntu-latest for workflow builds([e2e62b6](https://github.com/ofkm/arcane/commit/e2e62b6048ca75be27f9cfe1ebe9ba8abda59ec7) by @kmendell)
- unify project, env, and template file writing logic ([#725](https://github.com/ofkm/arcane/pull/725) by @kmendell)
- bump the dev-dependencies group with 2 updates ([#728](https://github.com/ofkm/arcane/pull/728) by @dependabot[bot])
- bump the prod-dependencies group with 5 updates ([#727](https://github.com/ofkm/arcane/pull/727) by @dependabot[bot])
- replace custom sheet with drawer component ([#726](https://github.com/ofkm/arcane/pull/726) by @kmendell)
- tweak dockerfile([e640439](https://github.com/ofkm/arcane/commit/e6404392c811bbf7dadd2c773f7f184c011c5360) by @kmendell)

**Full Changelog**: https://github.com/ofkm/arcane/compare/v1.5.2...v1.6.0

## v1.5.2

### Bug fixes

- remove custom timestamps and rely soley on dockers log timestamps([c69e00a](https://github.com/ofkm/arcane/commit/c69e00a30f9b546a3b095faef010bff746197e5d) by @kmendell)
- container metrics not respecting the cpu limit / count([f9840de](https://github.com/ofkm/arcane/commit/f9840de4f06cf5a844731af804b6b19d82bf0c8a) by @kmendell)
- registry credentials not reliably being passed to remote environments([31cfd54](https://github.com/ofkm/arcane/commit/31cfd540dbd7130876685f56055a361e9a7f1d68) by @kmendell)
- revert development compose deploy limits ([#697](https://github.com/ofkm/arcane/pull/697) by @cabaucom376)
- update registry template url to new domain([1ee3f1c](https://github.com/ofkm/arcane/commit/1ee3f1c7634b68dbb40969b30945659f18a71962) by @kmendell)

### Performance Improvements

- use service-worker and better caching headers to better manage cache problems after updates([3200b39](https://github.com/ofkm/arcane/commit/3200b395bcef1c7f9fee3954c8b327fbb3d023a9) by @kmendell)

### Other

- fix go import check([fd2a50b](https://github.com/ofkm/arcane/commit/fd2a50b49b4801f6fe682b3e878d64f2a87579fb) by @kmendell)

**Full Changelog**: https://github.com/ofkm/arcane/compare/v1.5.1...v1.5.2

## v1.5.1

### Bug fixes

- do not log \_app directory([a9efa58](https://github.com/ofkm/arcane/commit/a9efa586d5e20c3b6ea171adb6eccb7c9ef9c68e) by @kmendell)
- selected environment id not persisting across refreshes([d87db5b](https://github.com/ofkm/arcane/commit/d87db5b83cfa05c0a995dfb9a1bbf5219ad3e7c3) by @kmendell)
- accent color not reset after settings form reset([99fb83d](https://github.com/ofkm/arcane/commit/99fb83d273bcd4a4266a9ac8a735c67a1dc306d6) by @kmendell)
- project -> service back button not returning to project page([cbb6faa](https://github.com/ofkm/arcane/commit/cbb6faa9129cf5e9e950c635f3d0d68f2c9f459f) by @kmendell)
- rework project redeploy logic to use compose pull & compose up -d([a5e377b](https://github.com/ofkm/arcane/commit/a5e377bee5f1871133cd0fb5351758c4f12a43c4) by @kmendell)
- use project/container name in action toast messages([3a7ea34](https://github.com/ofkm/arcane/commit/3a7ea34e272d220b777642dc4a8134169867f711) by @kmendell)
- use accent color for code editor highlighter([ee6a0ad](https://github.com/ofkm/arcane/commit/ee6a0adb68fbb87386730351416811d57d702369) by @kmendell)
- proper light and dark modes for container terminal([5c624c1](https://github.com/ofkm/arcane/commit/5c624c1114b652c589ad8720aebd08130382800f) by @kmendell)
- tail container and project logs properly on all ws hubs([b861c19](https://github.com/ofkm/arcane/commit/b861c19757bcdbd52a4ee109e497a323900d0b66) by @kmendell)
- show exposed non published ports for containers([1dcc8b2](https://github.com/ofkm/arcane/commit/1dcc8b2c19dc7f53be28dedab276d3c850284f32) by @kmendell)
- log viewer column layout duplicating timestamps, and un-needed scrollbars([3a1aeb5](https://github.com/ofkm/arcane/commit/3a1aeb5b6b7cf26c92b9d39c5c7150f2c75f8946) by @kmendell)
- volumes not showing containers they are being used by([6eac860](https://github.com/ofkm/arcane/commit/6eac860bcc93f91b4d9d0708bbc1867f581377b3) by @kmendell)
- cache diskUsagePath setting to stop repeated database calls([4cd637b](https://github.com/ofkm/arcane/commit/4cd637b439bb19979eccfd99fcf4765e58f09ef8) by @kmendell)

### Other

- use parent layout settings to avoid duplicate api calls([fb89650](https://github.com/ofkm/arcane/commit/fb89650b37bb8756ecf3eb8febe6079ddf6d722b) by @kmendell)
- bump to 10.18.2([8812a58](https://github.com/ofkm/arcane/commit/8812a58d3e5f6978eaa861934f99ad4674c38bc7) by @kmendell)
- run formatter([57300b9](https://github.com/ofkm/arcane/commit/57300b9a5aac0ea3e1a413da2f9917099790ef21) by @kmendell)

**Full Changelog**: https://github.com/ofkm/arcane/compare/v1.5.0...v1.5.1

## v1.5.0

### New features

- docker socket proxy support via environment variable ([#616](https://github.com/ofkm/arcane/pull/616) by @Copilot)
- configurable disk usage path ([#650](https://github.com/ofkm/arcane/pull/650) by @kmendell)
- health status on project service card ([#658](https://github.com/ofkm/arcane/pull/658) by @kmendell)
- customizable accent color ([#678](https://github.com/ofkm/arcane/pull/678) by @kmendell)
- allow filtering events by severity([65c7c75](https://github.com/ofkm/arcane/commit/65c7c75d7f988e5fa59613abddf478ee13a2939a) by @kmendell)
- show volume size in table ([#685](https://github.com/ofkm/arcane/pull/685) by @kmendell)

### Bug fixes

- remove tag based update logic and only use digests ([#627](https://github.com/ofkm/arcane/pull/627) by @kmendell)
- allow both pkce and plain auth challenges([fb4ac74](https://github.com/ofkm/arcane/commit/fb4ac74c0897cb8a1eaabe54dd98a6fa88242d2e) by @kmendell)
- use 'up' and 'down' labels for project table actions([e9d8d38](https://github.com/ofkm/arcane/commit/e9d8d38507f9c5b22197e88f129f215ec155dfbc) by @kmendell)
- use proper --all filter for volume pruning([734015b](https://github.com/ofkm/arcane/commit/734015ba290d954c68349a3f65d8d873ac806155) by @kmendell)
- use correct response messages for container registry test ([#665](https://github.com/ofkm/arcane/pull/665) by @kmendell)
- memory leak in websocket connections ([#664](https://github.com/ofkm/arcane/pull/664) by @kmendell)
- default not able to be overidden([2d65952](https://github.com/ofkm/arcane/commit/2d65952525b5a94c19e107579aa817148cb3e068) by @kmendell)
- unable to set global env when ui config is disabled([dfb9192](https://github.com/ofkm/arcane/commit/dfb91924c57d5591aebb600843292cbfe94bc5e4) by @kmendell)
- project editor height on project creation page([7d1ff5b](https://github.com/ofkm/arcane/commit/7d1ff5b227636641a31ba64834d75fa6f753bee5) by @kmendell)
- add proper borders to event stat cards([4523985](https://github.com/ofkm/arcane/commit/45239859233d1185cdcf672113969682ef7a1c20) by @kmendell)

### Other

- fix ES translations ([#630](https://github.com/ofkm/arcane/pull/630) by @vsc55)
- bump the backend-dependencies group in /backend with 2 updates ([#646](https://github.com/ofkm/arcane/pull/646) by @dependabot[bot])
- move compose examples to seperate folder([aab7d3a](https://github.com/ofkm/arcane/commit/aab7d3ab966ab9b0697aeb9b93b3a11e5b752650) by @kmendell)
- update readme note([8f32754](https://github.com/ofkm/arcane/commit/8f32754f1e262334c9c4dd26f62c8c9ad29dfb32) by @kmendell)
- bump the prod-dependencies group with 4 updates ([#644](https://github.com/ofkm/arcane/pull/644) by @dependabot[bot])
- bump eslint from 9.36.0 to 9.37.0 in the dev-dependencies group ([#645](https://github.com/ofkm/arcane/pull/645) by @dependabot[bot])
- ensure proper handling of contexts and spec ([#647](https://github.com/ofkm/arcane/pull/647) by @kmendell)
- cleanup and simplify application bootstrap ([#651](https://github.com/ofkm/arcane/pull/651) by @kmendell)
- bump the prod-dependencies group with 4 updates ([#654](https://github.com/ofkm/arcane/pull/654) by @dependabot[bot])
- fix logic with e2e tests caching([f910e39](https://github.com/ofkm/arcane/commit/f910e3989104528e732921a53ca782133ab47629) by @kmendell)
- use up and down selectors([d3d0607](https://github.com/ofkm/arcane/commit/d3d060703a27b4301338764b59b855404db4bc22) by @kmendell)
- bump the dev-dependencies group with 2 updates ([#655](https://github.com/ofkm/arcane/pull/655) by @dependabot[bot])
- bump the prod-dependencies group with 3 updates ([#660](https://github.com/ofkm/arcane/pull/660) by @dependabot[bot])
- fix internal reponse of challenge manager([3d7187d](https://github.com/ofkm/arcane/commit/3d7187dda8b5b3cb578f010de6e593ca5b80b02c) by @kmendell)
- update readme with new logo([4b34d0c](https://github.com/ofkm/arcane/commit/4b34d0caa92ebb39c75217f91e03f18998c5c840) by @kmendell)
- serve images from backend vs static frontend assets ([#671](https://github.com/ofkm/arcane/pull/671) by @kmendell)
- bump the backend-dependencies group in /backend with 4 updates ([#667](https://github.com/ofkm/arcane/pull/667) by @dependabot[bot])
- bump the prod-dependencies group with 3 updates ([#666](https://github.com/ofkm/arcane/pull/666) by @dependabot[bot])
- bump @types/node from 24.7.0 to 24.7.1 in the dev-dependencies group ([#672](https://github.com/ofkm/arcane/pull/672) by @dependabot[bot])
- update pwa assets ([#673](https://github.com/ofkm/arcane/pull/673) by @kmendell)
- use docker build cloud([01a6dc2](https://github.com/ofkm/arcane/commit/01a6dc217eb0adaf02113376c35e109ecff75b77) by @kmendell)
- add docker build cloud for release and e2e tests([7a981ce](https://github.com/ofkm/arcane/commit/7a981ce1231c383460c03c42eecfc7462e1add2d) by @kmendell)
- remove docker build cloud for e2e tests([2675285](https://github.com/ofkm/arcane/commit/2675285f0013fb696bdd69ffb3e05c12108ff0e5) by @kmendell)
- improve types on loading indicator([2118d6f](https://github.com/ofkm/arcane/commit/2118d6fddcf7409b497728c85fe27db93a529465) by @kmendell)
- use root page for /customize route ([#675](https://github.com/ofkm/arcane/pull/675) by @kmendell)
- use new empty component([dad95bc](https://github.com/ofkm/arcane/commit/dad95bc371205816a22f7af123131c4dfe444904) by @kmendell)
- use shadcn spinner component ([#676](https://github.com/ofkm/arcane/pull/676) by @kmendell)
- update global variable input design([87f60d6](https://github.com/ofkm/arcane/commit/87f60d696b98651c8ae252e6d1d5b974cb461786) by @kmendell)
- bump github.com/quic-go/quic-go from 0.54.0 to 0.54.1 in /backend in the go_modules group across 1 directory ([#679](https://github.com/ofkm/arcane/pull/679) by @dependabot[bot])
- cleanup svelte components ([#681](https://github.com/ofkm/arcane/pull/681) by @kmendell)
- remove stat cards from users page([bfcfc84](https://github.com/ofkm/arcane/commit/bfcfc849ecfe2023caa5d074e854d1e4e5c3f502) by @kmendell)
- simplify stat card([319557e](https://github.com/ofkm/arcane/commit/319557eb723faa68a666af0175d8424bcb65881f) by @kmendell)
- skip volume test if no inuse volumes([324edbf](https://github.com/ofkm/arcane/commit/324edbf380969f9a396f139224b0225c851dffd3) by @kmendell)

**Full Changelog**: https://github.com/ofkm/arcane/compare/v1.4.0...v1.5.0

## v1.4.0

### New features

- interactive shell access to containers ([#611](https://github.com/ofkm/arcane/pull/611) by @Copilot)
- container stats websocket, and overhaul ([#612](https://github.com/ofkm/arcane/pull/612) by @kmendell)
- configure settings on environments via the web ui ([#613](https://github.com/ofkm/arcane/pull/613) by @kmendell)
- global compose and env defaults configuration ([#617](https://github.com/ofkm/arcane/pull/617) by @kmendell)
- global environment variables ([#620](https://github.com/ofkm/arcane/pull/620) by @cabaucom376)
- mobile responsive tables ([#597](https://github.com/ofkm/arcane/pull/597) by @cabaucom376)
- option to group containers by project ([#625](https://github.com/ofkm/arcane/pull/625) by @cabaucom376)

### Bug fixes

- remove trailing slashing for all resource links([1d2e41c](https://github.com/ofkm/arcane/commit/1d2e41c0030b29bedb2ecb93cdc5740c88c32b52) by @kmendell)
- only show docker / general settings on environment page([57cff79](https://github.com/ofkm/arcane/commit/57cff793ba3cd32b6c4d3e3bdcb0c4a5d4d470ec) by @kmendell)
- add missing context([ba4d20b](https://github.com/ofkm/arcane/commit/ba4d20b78a252a17ebfc479841e5566411e60671) by @kmendell)

### Other

- fix locators for container tests([10edce8](https://github.com/ofkm/arcane/commit/10edce81d7c40111cb1701d3ab658fbf8a6f912b) by @kmendell)
- bump the prod-dependencies group with 4 updates ([#614](https://github.com/ofkm/arcane/pull/614) by @dependabot[bot])
- update readme screenshot([a18e3c0](https://github.com/ofkm/arcane/commit/a18e3c0090a27de6c608aeeb9d6ec11a53313f66) by @kmendell)
- bump the backend-dependencies group in /backend with 2 updates ([#623](https://github.com/ofkm/arcane/pull/623) by @dependabot[bot])
- bump the prod-dependencies group with 4 updates ([#622](https://github.com/ofkm/arcane/pull/622) by @dependabot[bot])
- bump the dev-dependencies group across 1 directory with 2 updates ([#621](https://github.com/ofkm/arcane/pull/621) by @dependabot[bot])

**Full Changelog**: https://github.com/ofkm/arcane/compare/v1.3.0...v1.4.0

## v1.3.0

### New features

- mobile navigation ([#537](https://github.com/ofkm/arcane/pull/537) by @cabaucom376)
- redesigned dashboard stats and docker info cards ([#607](https://github.com/ofkm/arcane/pull/607) by @kmendell)

### Bug fixes

- correct display of user label in event details ([#563](https://github.com/ofkm/arcane/pull/563) by @neilsb)
- use display: fullscreen for app.webmanifest([d711d44](https://github.com/ofkm/arcane/commit/d711d44831e740be72f5e87c43e64805fe3ce12a) by @kmendell)
- refresh container list when environment changes ([#573](https://github.com/ofkm/arcane/pull/573) by @neilsb)
- environment handler not registering correctly([05a1806](https://github.com/ofkm/arcane/commit/05a180686c7cd2aaab4b1f2cb4912d73ff70f67a) by @kmendell)
- remove unused argument for environment handler([b3effbb](https://github.com/ofkm/arcane/commit/b3effbbad1bc65e75088b70998fcc66c4711eb08) by @kmendell)
- add work directory and config files labels to projects([1c25aa8](https://github.com/ofkm/arcane/commit/1c25aa840f59c6f9392b7e28dec9373c8e84534c) by @kmendell)
- properly align login screen elements when only oidc is enabled([4aeb845](https://github.com/ofkm/arcane/commit/4aeb845d5289cc0580a918fa045b08553324562f) by @kmendell)
- sort and filter properly using docker api ([#593](https://github.com/ofkm/arcane/pull/593) by @kmendell)
- project title not saving([fe4e8b4](https://github.com/ofkm/arcane/commit/fe4e8b43a4160a56470c1082eef2e3822fdb0bce) by @kmendell)
- use live project status vs stored value([1426374](https://github.com/ofkm/arcane/commit/14263744ebbc899b649fc205316dfe00599c4f84) by @kmendell)
- image update item not retreving the correct update data([d6c449c](https://github.com/ofkm/arcane/commit/d6c449cabae66a87a94e31d7ebf604283e5a9568) by @kmendell)
- skip onboarding when UI configuration is disabled ([#602](https://github.com/ofkm/arcane/pull/602) by @kmendell)
- pass registry credentials to environments ([#603](https://github.com/ofkm/arcane/pull/603) by @kmendell)
- remove trailing slash on projectId route([f824aa4](https://github.com/ofkm/arcane/commit/f824aa4d4dc87145a0ad771c2ce9567f25a7f807) by @kmendell)
- use correct destructive variant for registry delete item([cd4a49b](https://github.com/ofkm/arcane/commit/cd4a49ba5b352e36f3b672161a9024d7b871c3a7) by @kmendell)
- use correct status badge for container registry table([9fdbe02](https://github.com/ofkm/arcane/commit/9fdbe025de0bf4bb68129b6f1c09a65a17bd2c0f) by @kmendell)
- restrict docker dialog to screen bounds([2f74a3e](https://github.com/ofkm/arcane/commit/2f74a3e74df15d4992a00bb568437b1e09c79761) by @kmendell)

### Other

- bump @playwright/test from 1.55.0 to 1.55.1 in the dev-dependencies group ([#560](https://github.com/ofkm/arcane/pull/560) by @dependabot[bot])
- bump the prod-dependencies group across 1 directory with 5 updates ([#565](https://github.com/ofkm/arcane/pull/565) by @dependabot[bot])
- bump github.com/go-co-op/gocron/v2 from 2.16.5 to 2.16.6 in /backend in the backend-dependencies group ([#570](https://github.com/ofkm/arcane/pull/570) by @dependabot[bot])
- generalize environment routes into individual handlers ([#568](https://github.com/ofkm/arcane/pull/568) by @kmendell)
- non env endpoints registered before env middleware ([#580](https://github.com/ofkm/arcane/pull/580) by @kmendell)
- bump the prod-dependencies group across 1 directory with 5 updates ([#577](https://github.com/ofkm/arcane/pull/577) by @dependabot[bot])
- use coreos/go-oidc library for oidc auth([80317bb](https://github.com/ofkm/arcane/commit/80317bbf9301cd70d9a2e5900643650a359c5eb8) by @kmendell)
- add APP_URL to .env.dev([17738b1](https://github.com/ofkm/arcane/commit/17738b181af781c9045ba6ab7147d485adaf2fb8) by @kmendell)
- improve ui consistency and maintainability ([#582](https://github.com/ofkm/arcane/pull/582) by @cabaucom376)
- bump @codemirror/view from 6.38.3 to 6.38.4 in the prod-dependencies group ([#594](https://github.com/ofkm/arcane/pull/594) by @dependabot[bot])
- update package manager to 10.17.1([6b287e8](https://github.com/ofkm/arcane/commit/6b287e8accbac215897514b87766c2fcf8f52c69) by @kmendell)
- add CODEOWNERS([c621fab](https://github.com/ofkm/arcane/commit/c621fab260156eb757bb0f81c5ac9b422034ca00) by @kmendell)
- tweak the styling of resource detail pages ([#595](https://github.com/ofkm/arcane/pull/595) by @cabaucom376)
- enhance the development pipeline by incorporating caching and troubleshooting tools ([#596](https://github.com/ofkm/arcane/pull/596) by @cabaucom376)
- bump typescript-eslint from 8.44.1 to 8.45.0 in the prod-dependencies group ([#599](https://github.com/ofkm/arcane/pull/599) by @dependabot[bot])
- bump @types/node from 24.5.2 to 24.6.0 in the dev-dependencies group ([#598](https://github.com/ofkm/arcane/pull/598) by @dependabot[bot])
- bump the dev-dependencies group with 2 updates ([#606](https://github.com/ofkm/arcane/pull/606) by @dependabot[bot])
- bump the prod-dependencies group with 3 updates ([#605](https://github.com/ofkm/arcane/pull/605) by @dependabot[bot])

**Full Changelog**: https://github.com/ofkm/arcane/compare/v1.2.2...v1.3.0

## v1.2.2

### Bug fixes

- container registry test connection not checking the correct endpoint([9d2c251](https://github.com/ofkm/arcane/commit/9d2c2513a2f6b610d895002af00ade1a0f1c0cc5) by @kmendell)
- project save button not the correct size([7872080](https://github.com/ofkm/arcane/commit/787208046bebf153ad2fbc807f921a119e60723f) by @kmendell)
- use correct headers and cors values for websockets ([#553](https://github.com/ofkm/arcane/pull/553) by @kmendell)
- remove project updater logic, prune images after updating ([#556](https://github.com/ofkm/arcane/pull/556) by @kmendell)
- don't override env vars with default settings ([#558](https://github.com/ofkm/arcane/pull/558) by @kmendell)
- use correct json body for system prune([13e35fd](https://github.com/ofkm/arcane/commit/13e35fdcb7541bdfcecc8f85a01802d50cf723f9) by @kmendell)

### Performance Improvements

- optimize dockerfile([f2e8bd3](https://github.com/ofkm/arcane/commit/f2e8bd375f2b8f4c852b13d235d72f15f9bdf411) by @kmendell)

### Other

- add pull request title validation([e129344](https://github.com/ofkm/arcane/commit/e1293448843f429cd6510594575ec564a581a68c) by @kmendell)
- add recommended extensions([539e00b](https://github.com/ofkm/arcane/commit/539e00ba00b668b5ff573a9b36d354782b03f7a0) by @kmendell)
- .github/workflows: Migrate workflows to Blacksmith runners ([#555](https://github.com/ofkm/arcane/pull/555) by @blacksmith-sh[bot])
- extract digest retrieval logic into a separate function([5b012ff](https://github.com/ofkm/arcane/commit/5b012ffa51a237eeb3efdad1c09ce1180abade07) by @kmendell)
- remove blacksmith runners([0b4fee5](https://github.com/ofkm/arcane/commit/0b4fee5309cf9b5e5153337e59495b70562869cb) by @kmendell)
- fix go linter([274cb4a](https://github.com/ofkm/arcane/commit/274cb4af8ca9ee24624ab642fc601a278880fcb7) by @kmendell)
- ignore linter on init function([b3d5974](https://github.com/ofkm/arcane/commit/b3d5974fd9d4d54476265ee4bab6e8e1e9b9385f) by @kmendell)

**Full Changelog**: https://github.com/ofkm/arcane/compare/v1.2.1...v1.2.2

## v1.2.1

### Bug fixes

- use correct settings return type([02db09a](https://github.com/ofkm/arcane/commit/02db09a0402c62087cf3a6aa89b80fe684b3f9d9) by @kmendell)
- use correct running container count in dashboard metric([98a9cfa](https://github.com/ofkm/arcane/commit/98a9cfafb5505553252fc88b4f45dbf3d7ae3eb4) by @kmendell)
- show correct stats for remote environments on dashboard ([#549](https://github.com/ofkm/arcane/pull/549) by @kmendell)

### Dependencies

- bump the backend-dependencies group in /backend with 3 updates ([#540](https://github.com/ofkm/arcane/pull/540) by @dependabot[bot])
- bump the prod-dependencies group with 6 updates ([#548](https://github.com/ofkm/arcane/pull/548) by @dependabot[bot])

### Other

- fix changelog with correct information([f20de40](https://github.com/ofkm/arcane/commit/f20de405d07377a5d0974a37f6938047c5bfa108) by @kmendell)
- bump the prod-dependencies group with 6 updates ([#538](https://github.com/ofkm/arcane/pull/538) by @dependabot[bot])
- bump the dev-dependencies group across 1 directory with 3 updates ([#545](https://github.com/ofkm/arcane/pull/545) by @dependabot[bot])
- add concurrency settings to E2E tests workflow([fe7ee76](https://github.com/ofkm/arcane/commit/fe7ee76dec653fc7f00874b26d613e903e138234) by @kmendell)

**Full Changelog**: https://github.com/ofkm/arcane/compare/v1.2.0...v1.2.1

## v1.2.0

### New features

- analytics plugin for number of instances ([#483](https://github.com/ofkm/arcane/pull/483) by @kmendell)
- use websockets for streaming logs ([#495](https://github.com/ofkm/arcane/pull/495) by @kmendell)
- use websockets for dashboard stats ([#496](https://github.com/ofkm/arcane/pull/496) by @kmendell)
- add pull progress popover when deploying project ([#512](https://github.com/ofkm/arcane/pull/512) by @kmendell)
- add build cache checkbox to prune dialog([5db08fc](https://github.com/ofkm/arcane/commit/5db08fc2ea41630cac69ba15c42bfcda885d4f83) by @kmendell)
- screen responsive navigation ([#516](https://github.com/ofkm/arcane/pull/516) by @cabaucom376)
- redesigned projects page ([#464](https://github.com/ofkm/arcane/pull/464) by @kmendell)
- settings page ui refresh ([#518](https://github.com/ofkm/arcane/pull/518) by @cabaucom376)
- ui configuration from env variables ([#526](https://github.com/ofkm/arcane/pull/526) by @kmendell)
- support PWA icons ([#529](https://github.com/ofkm/arcane/pull/529) by @cabaucom376)

### Bug fixes

- update default admin user printed in logs, also print the default admin password on first run([5d3a66c](https://github.com/ofkm/arcane/commit/5d3a66cdce07cb8cd509be729a880a8e81877ac9) by @kmendell)
- update network usage detection to account for networks in use([4170985](https://github.com/ofkm/arcane/commit/417098513f0621bcc2e4cd7c7040994a4187a702) by @kmendell)
- use distribution reference for image digests ([#484](https://github.com/ofkm/arcane/pull/484) by @kmendell)
- load working directory before dotenv for projects([232d62e](https://github.com/ofkm/arcane/commit/232d62ec19c191873861e31bfb27be08401fa512) by @kmendell)
- correctly validate and parse compose projects on load ([#492](https://github.com/ofkm/arcane/pull/492) by @kmendell)
- reload interface when switching environments([53c0aa4](https://github.com/ofkm/arcane/commit/53c0aa43dc7d43582a4b64ef3a4618613178d004) by @kmendell)
- dashboard not showing remote environment information([3a7b858](https://github.com/ofkm/arcane/commit/3a7b858a89dee7948326542796afdcbfa993bf9b) by @kmendell)
- image polling value allows any number ([#501](https://github.com/ofkm/arcane/pull/501) by @kmendell)
- password change not being accepted by the backend([e82b114](https://github.com/ofkm/arcane/commit/e82b11489acd8d790ed8573ba32079561d8ccf97) by @kmendell)
- use better wording on project action buttons and redploy dialog([bb25ac2](https://github.com/ofkm/arcane/commit/bb25ac240327054a444d220b721f33af98ff3179) by @kmendell)
- only watch env or compose files in filesystem watcher job([c957681](https://github.com/ofkm/arcane/commit/c957681dc46e43e7f963d22a4721499056def220) by @kmendell)
- templates wouldnt allow empty files to be loaded in the ui([33b8303](https://github.com/ofkm/arcane/commit/33b8303b09b126de76bb008eae5be2ec6e6336f2) by @kmendell)
- use projectId for saving instead of projectName([1cb7ab2](https://github.com/ofkm/arcane/commit/1cb7ab2f93a86f281903e3b72726e6c5e9be4794) by @kmendell)
- ignore root files in template directory([637460a](https://github.com/ofkm/arcane/commit/637460ab4628719a298d7790e57193da084107dd) by @kmendell)
- minor styling issue ([#528](https://github.com/ofkm/arcane/pull/528) by @cabaucom376)
- dont skip .env resolution([d8af378](https://github.com/ofkm/arcane/commit/d8af378f04dd1081f51d9aa3f06f81cdeae2b65d) by @kmendell)

### Performance Improvements

- improve websocket logs performance ([#534](https://github.com/ofkm/arcane/pull/534) by @kmendell)

### Dependencies

- bump the prod-dependencies group with 4 updates ([#480](https://github.com/ofkm/arcane/pull/480) by @dependabot[bot])
- bump the backend-dependencies group across 1 directory with 2 updates([ec7e6cb](https://github.com/ofkm/arcane/commit/ec7e6cb725e7cab70a6c9cd0da5664c4dba5d3a1) by @dependabot[bot])
- bump the backend-dependencies group across 1 directory with 2 updates ([#485](https://github.com/ofkm/arcane/pull/485) by @dependabot[bot])
- bump the prod-dependencies group with 7 updates ([#522](https://github.com/ofkm/arcane/pull/522) by @dependabot[bot])

### Other

- add service unit tests([99e8a68](https://github.com/ofkm/arcane/commit/99e8a68d87dcc815d3b4b66e5998a77cfa7451ab) by @kmendell)
- fix lints in tests([d3d50fb](https://github.com/ofkm/arcane/commit/d3d50fbf943b9bdb1b8804ae8e3f8fbfbf30731f) by @kmendell)
- move the app version check to the backend ([#473](https://github.com/ofkm/arcane/pull/473) by @kmendell)
- inject default http client into services([441f8a4](https://github.com/ofkm/arcane/commit/441f8a429798c250c9cf52442ed7a883740d2582) by @kmendell)
- make helper in loading projects([694607c](https://github.com/ofkm/arcane/commit/694607c4a4e7b5a2de916e28ece7372435b00431) by @kmendell)
- bump @types/node in the dev-dependencies group([e9f2f74](https://github.com/ofkm/arcane/commit/e9f2f7444bd5efe18da4251cc544b6c798e4f148) by @dependabot[bot])
- bump @types/node from 24.3.1 to 24.4.0 in the dev-dependencies group ([#481](https://github.com/ofkm/arcane/pull/481) by @dependabot[bot])
- use correct analytics host([c44aaec](https://github.com/ofkm/arcane/commit/c44aaec2b92bf191a5e684624f38bbff9c25a5fd) by @kmendell)
- bump @types/node from 24.4.0 to 24.5.0 in the dev-dependencies group ([#494](https://github.com/ofkm/arcane/pull/494) by @dependabot[bot])
- bump the prod-dependencies group across 1 directory with 3 updates ([#499](https://github.com/ofkm/arcane/pull/499) by @dependabot[bot])
- add download docker script([d61d12a](https://github.com/ofkm/arcane/commit/d61d12a95e04d9428d7f98f9681a2e2af4c0c7c4) by @kmendell)
- use compose-go for project releated functions ([#508](https://github.com/ofkm/arcane/pull/508) by @kmendell)
- add ARG TARGETARCH back to static docker files([86e799f](https://github.com/ofkm/arcane/commit/86e799f288c784c2b9534a8a9b7711a868e0aeee) by @kmendell)
- bump the prod-dependencies group with 3 updates ([#504](https://github.com/ofkm/arcane/pull/504) by @dependabot[bot])
- container-based development workflow with hot reload and VS Code integration ([#509](https://github.com/ofkm/arcane/pull/509) by @cabaucom376)
- cleanup go module([932824c](https://github.com/ofkm/arcane/commit/932824c1ba805a663512b8c19fa87330253dcd93) by @kmendell)
- Auto close VSCode terminals when tasks are finished ([#511](https://github.com/ofkm/arcane/pull/511) by @cabaucom376)
- include version in changelog([80fd46e](https://github.com/ofkm/arcane/commit/80fd46eea69ee92c082288361215b18a9e942900) by @kmendell)
- use local build for arcane-agent([f9a962e](https://github.com/ofkm/arcane/commit/f9a962e63ed4e7e7b10e1951726c6605fc4fe83f) by @kmendell)
- split logic for stats ws([d43a256](https://github.com/ofkm/arcane/commit/d43a25650692290b7b9ca03e30c6fb802ad4c2ac) by @kmendell)
- bump svelte from 5.38.10 to 5.39.1 in the prod-dependencies group ([#513](https://github.com/ofkm/arcane/pull/513) by @dependabot[bot])
- bump the dev-dependencies group with 2 updates ([#505](https://github.com/ofkm/arcane/pull/505) by @dependabot[bot])
- add merge conflict labeler action([04a6c2b](https://github.com/ofkm/arcane/commit/04a6c2b313a1815ff41dbf536ab9db0101602886) by @kmendell)
- simplify filesystem watcher([ae159f9](https://github.com/ofkm/arcane/commit/ae159f9d38faf191f9f64cc6db31455162fb4947) by @kmendell)
- cleanup sidebar translation keys([b5615c4](https://github.com/ofkm/arcane/commit/b5615c449a996516f6bc0037b5670310c5bed193) by @kmendell)
- fix updated sidebar text([2440475](https://github.com/ofkm/arcane/commit/24404754d98820c1fddf6c890289a3089b471337) by @kmendell)
- update projects header to new key([4912746](https://github.com/ofkm/arcane/commit/49127464754cd920e3215790ca375b543dfbbe5a) by @kmendell)
- bump vite from 7.1.5 to 7.1.6 in the dev-dependencies group ([#521](https://github.com/ofkm/arcane/pull/521) by @dependabot[bot])
- cleanup template service ([#523](https://github.com/ofkm/arcane/pull/523) by @kmendell)
- remove dead and unused code ([#525](https://github.com/ofkm/arcane/pull/525) by @kmendell)
- use git cliff for releases (thanks pocket-id :))([d742817](https://github.com/ofkm/arcane/commit/d742817ccd9c01abeaed366bb87c1576f67e4f0f) by @kmendell)
- refine settings pages ([#532](https://github.com/ofkm/arcane/pull/532) by @cabaucom376)
- update cliff.toml to include deps([9101ca3](https://github.com/ofkm/arcane/commit/9101ca3113edb284b0c188128e611a86f00c06df) by @kmendell)
- fix builds for release([191ffc1](https://github.com/ofkm/arcane/commit/191ffc1bc0b713f9114ec7e0bb6904efa9dbf6ad) by @kmendell)
- only build mac binaires on mac host([b60a87d](https://github.com/ofkm/arcane/commit/b60a87d2c4d3369c25ee45f475495cb737f1aea9) by @kmendell)

**Full Changelog**: https://github.com/ofkm/arcane/compare/v1.1.0...v1.2.0

## v1.1.0

### New features

- persistent table filters, page size, and column headers ([#449](https://github.com/ofkm/arcane/pull/449) by @kmendell)
- move quick actions to the header for more simplified look([f0b43b6](https://github.com/ofkm/arcane/commit/f0b43b67f645c1ef842e5ce215dd8e8c349fe573) by @kmendell)
- container port links and overview ([#457](https://github.com/ofkm/arcane/pull/457) by @kmendell)
- allow underscores in project names([2b464db](https://github.com/ofkm/arcane/commit/2b464db4a851eaba3e88295237f0be0b7f350815) by @kmendell)
- add usage badge to network table([6019045](https://github.com/ofkm/arcane/commit/60190456dd4687ac360610dd3c9a941c7d36d2a3) by @kmendell)

### Bug fixes

- check for updates only checking the current page of images([0da46a0](https://github.com/ofkm/arcane/commit/0da46a046aabfcc2721996583a8ad73ccf07277c) by @kmendell)
- do not recursive chown the /app/data/projects directory in entrypoint([368612a](https://github.com/ofkm/arcane/commit/368612a8f072e59d63745a700f6de9f8c588b033) by @kmendell)
- make auto-update off by defualt on fresh installs([129c5c5](https://github.com/ofkm/arcane/commit/129c5c5e1f8e69efbc8b9af496baa56046e3b6e4) by @kmendell)
- dont allow the auto updater to update arcane it self([4b0931e](https://github.com/ofkm/arcane/commit/4b0931e52935580d3040aced090ce8c39850d51b) by @kmendell)
- do not stop arcanes container it self when using the quick action([0166084](https://github.com/ofkm/arcane/commit/016608442b052df71ede8c94dff1346c4e4f6551) by @kmendell)
- project save button not using the correct styles([9825c4a](https://github.com/ofkm/arcane/commit/9825c4aa15a59c626580e5aa6f6d39cc23843613) by @kmendell)
- use correct time for auto update job([e7a5a31](https://github.com/ofkm/arcane/commit/e7a5a3173f8f24ceccf34497915e1b845d377b43) by @kmendell)
- use correct running container count on dashboard([3dad327](https://github.com/ofkm/arcane/commit/3dad32721a0e53e6f44e2f73959e1d315835d36a) by @kmendell)
- projects are not searchable([31ce3e9](https://github.com/ofkm/arcane/commit/31ce3e9866981f16cb2e838c7e419ef332c67249) by @kmendell)
- add /api/health endpoint to agent mode([83f0bc0](https://github.com/ofkm/arcane/commit/83f0bc0b3305ecbc420b05968bfe79f3dd47c344) by @kmendell)
- update banner link not clickable([69e95e0](https://github.com/ofkm/arcane/commit/69e95e0457f5a317f1b7928039bd3719dd70471d) by @kmendell)
- allow use of the local templates directory ([#462](https://github.com/ofkm/arcane/pull/462) by @kmendell)
- reschedule jobs when polling or autoupdate job settings is changed([5c3f168](https://github.com/ofkm/arcane/commit/5c3f1687dd6ab5dfb06526fb2af9b40693e60b2c) by @kmendell)

### Dependencies

- bump the prod-dependencies group across 1 directory with 6 updates ([#446](https://github.com/ofkm/arcane/pull/446) by @dependabot[bot])

### Other

- add newer zsh release script([f91fe88](https://github.com/ofkm/arcane/commit/f91fe88ee120bcfc6e72e19b7bfe8999cc919aa3) by @kmendell)
- bump package manager version to pnpm 10.16.0([1313e54](https://github.com/ofkm/arcane/commit/1313e54c3680da9ec909b2a56e89f916e43b9ac3) by @kmendell)
- remove un-needed alert for auto update and polling enabled([f2606b7](https://github.com/ofkm/arcane/commit/f2606b750af2c8f2d6cd500590073f87ae1d6886) by @kmendell)
- cleanup old service port logic on compose page([34b87c7](https://github.com/ofkm/arcane/commit/34b87c767be9cd81eb2f2d38f1772c5b6f880e1b) by @kmendell)
- fix linter([4f1c9a9](https://github.com/ofkm/arcane/commit/4f1c9a9d8cd2c7e188e8369e3593e39732310554) by @kmendell)

**Full Changelog**: https://github.com/ofkm/arcane/compare/v1.0.2...v1.1.0

## v1.0.2

### Bug fixes

- streamline image reference handling by stripping digests and ensuring tags([c2a3b7d](https://github.com/ofkm/arcane/commit/c2a3b7d566b0590bc2b7e6cd9af5d79a5d20192b) by @kmendell)
- volume usage displays incorrectly([eb92b4f](https://github.com/ofkm/arcane/commit/eb92b4f3d0862cfa3848adadf7dc393fa9c99746) by @kmendell)
- dashboard meters not using consistant styling in light/dark mode([21a93f5](https://github.com/ofkm/arcane/commit/21a93f528ff53fc830d0504f7994ff932503d3c6) by @kmendell)
- update layout of sidebar and fix the missing label for language selector([2150088](https://github.com/ofkm/arcane/commit/215008860f781ca79c203b295ce04b674222d941) by @kmendell)
- show correct counts for stat cards ([#437](https://github.com/ofkm/arcane/pull/437) by @kmendell)
- update learn more link for templates page([60e3d9b](https://github.com/ofkm/arcane/commit/60e3d9bba72533963599517e2cbf48941443c080) by @kmendell)
- remove extra image id cell in image table([d288295](https://github.com/ofkm/arcane/commit/d288295360eda0286237b8586d257aae691a59d9) by @kmendell)
- volume usage filters not filtering volumes([2d8437d](https://github.com/ofkm/arcane/commit/2d8437dc41b772074fcc8a34b8e471a77baa497d) by @kmendell)
- image pruning not respecting selected prune mode([e8b9207](https://github.com/ofkm/arcane/commit/e8b9207fafeea1526513d6f51abed1751c6a3d88) by @kmendell)

### Dependencies

- bump actions/attest-build-provenance from 2 to 3 ([#410](https://github.com/ofkm/arcane/pull/410) by @dependabot[bot])
- bump the go_modules group across 1 directory with 2 updates ([#412](https://github.com/ofkm/arcane/pull/412) by @dependabot[bot])
- bump the backend-dependencies group in /backend with 9 updates ([#414](https://github.com/ofkm/arcane/pull/414) by @dependabot[bot])
- bump the frontend-major-updates group with 2 updates ([#421](https://github.com/ofkm/arcane/pull/421) by @dependabot[bot])
- bump actions/download-artifact from 4 to 5 ([#411](https://github.com/ofkm/arcane/pull/411) by @dependabot[bot])
- bump the backend-dependencies group in /backend with 2 updates ([#436](https://github.com/ofkm/arcane/pull/436) by @dependabot[bot])

### Other

- fix release script([bf03a23](https://github.com/ofkm/arcane/commit/bf03a23184e7d1fc8f80c6f07d2fa3d9a5f52f70) by @kmendell)
- fix changelog([4882ad4](https://github.com/ofkm/arcane/commit/4882ad48cc7f2c733b0f0ae92fca838ad04ca156) by @kmendell)
- add discord link([2bfe94a](https://github.com/ofkm/arcane/commit/2bfe94af2e9fcfa267170fdda8b9c4bc295398ff) by @kmendell)
- update docker-compose.yml ([#426](https://github.com/ofkm/arcane/pull/426) by @sugarfunk)
- update agent docker compose([a7d464f](https://github.com/ofkm/arcane/commit/a7d464f036a84b7e2785d38773c39dfd1566b408) by @kmendell)
- bump the dev-dependencies group with 5 updates ([#420](https://github.com/ofkm/arcane/pull/420) by @dependabot[bot])
- remove 1.0 feedback issue([6b3f073](https://github.com/ofkm/arcane/commit/6b3f073b5eeacee159d4cfbc0450ea95f0c91966) by @kmendell)
- bump the prod-dependencies group with 13 updates ([#419](https://github.com/ofkm/arcane/pull/419) by @dependabot[bot])
- add Chinese files([e62083c](https://github.com/ofkm/arcane/commit/e62083c57c8dba0fe3666980579729a978365f17) by @kmendell)
- fix go linter([2bb66a0](https://github.com/ofkm/arcane/commit/2bb66a00b267f4800d249bd40f3d3f19fab6b545) by @kmendell)
- fix svelte check issues for code editor([2fc6c3b](https://github.com/ofkm/arcane/commit/2fc6c3ba5d2bb45958960cc086c8ecdede84aaf8) by @kmendell)
- make action buttons bindable([7b1c8f3](https://github.com/ofkm/arcane/commit/7b1c8f3e3053cc5ce01fadcd5f263f0074f58046) by @kmendell)
- refcator release workflow to have builds run in parallel([583c767](https://github.com/ofkm/arcane/commit/583c767fa44cb179664b7cd85267e892b8b04b31) by @kmendell)
- add Esperanto and French files([1daa17e](https://github.com/ofkm/arcane/commit/1daa17e953195a525300d6f34549dc019ac4c6cf) by @kmendell)
- update french name([6d9f43f](https://github.com/ofkm/arcane/commit/6d9f43f7b5587f4eca39275b4f65fa0d95281e34) by @kmendell)
- add missing translation([b308d36](https://github.com/ofkm/arcane/commit/b308d36cbed722e968c438342f41e70fe7b9eb70) by @kmendell)
- bump the prod-dependencies group with 3 updates ([#433](https://github.com/ofkm/arcane/pull/433) by @dependabot[bot])
- bump eslint-plugin-svelte from 3.12.2 to 3.12.3 in the dev-dependencies group ([#434](https://github.com/ofkm/arcane/pull/434) by @dependabot[bot])
- consolidate registry logic into a cleaner structure ([#443](https://github.com/ofkm/arcane/pull/443) by @kmendell)
- fix publish-release setp in releae workflow([a7ac9dc](https://github.com/ofkm/arcane/commit/a7ac9dc52937e67c4b32c91a0615e665e7aa2b43) by @kmendell)
- login before container attestations([eeb1d63](https://github.com/ofkm/arcane/commit/eeb1d63c887e94dda2a86180c3afc24fb0428026) by @kmendell)

**Full Changelog**: https://github.com/ofkm/arcane/compare/v1.0.1...v1.0.2

## v1.0.1

### Bug fixes

- register project handler to fix projects not pulling([038ebf6](https://github.com/ofkm/arcane/commit/038ebf61e9e8bff6ec899e9591e9c6fdb4c3db86) by @kmendell)
- onboarding not showing on fresh installs([51fa03a](https://github.com/ofkm/arcane/commit/51fa03ac6e61e5820963208c5e16977ccbfb9bd5) by @kmendell)
- onboarding flow not using correct values([c75839c](https://github.com/ofkm/arcane/commit/c75839c7d9910a71da971b87b9745af24fc8adb5) by @kmendell)
- use non secure cookie if running on http([fb2e6d9](https://github.com/ofkm/arcane/commit/fb2e6d9fd61918b615fabc45f42400ca10e6660c) by @kmendell)
- allow both http and https cookie names([b3b9f71](https://github.com/ofkm/arcane/commit/b3b9f7159ed51e388dbd208b070f35ded6ec48df) by @kmendell)

### Dependencies

- bump actions/setup-go from 5 to 6 ([#418](https://github.com/ofkm/arcane/pull/418) by @dependabot[bot])
- bump actions/cache from 3 to 4 ([#408](https://github.com/ofkm/arcane/pull/408) by @dependabot[bot])
- bump devalue to 5.3.2([5bb0880](https://github.com/ofkm/arcane/commit/5bb08809f5f4a6de80a6b8945e3992287dcb2d44) by @kmendell)

### Other

- fix dependabot paths([197ca6c](https://github.com/ofkm/arcane/commit/197ca6cfcb2d78869240d82d7586edc4fa4ce090) by @kmendell)
- use static build for agent-next image([75f14fd](https://github.com/ofkm/arcane/commit/75f14fda40767a0b85833120e9578148a075abc3) by @kmendell)

**Full Changelog**: https://github.com/ofkm/arcane/compare/v1.0.0...v1.0.1

## v1.0.0

### Bug fixes

- project files not removed when deleteing from the ui([65c93c6](https://github.com/ofkm/arcane/commit/65c93c6f91f9ed922f5584d158264c80f4607385) by @kmendell)

### Other

- update workflow to set dockerfile path([3f21d64](https://github.com/ofkm/arcane/commit/3f21d64661678fb12556eb7b8c0873b2f70ab2fd) by @kmendell)
- add agent preview workflow([f5e838d](https://github.com/ofkm/arcane/commit/f5e838dcb1f83c804abc21ddc5071f6f006b273c) by @kmendell)
- combined preview workflows([52386d9](https://github.com/ofkm/arcane/commit/52386d9ef2855baea6d486e8f748ef166a58fed3) by @kmendell)
- refactor encryption logic([3e59fc5](https://github.com/ofkm/arcane/commit/3e59fc54bda01bcdedeae8dc7f441905fe99a11b) by @kmendell)
- move health check to dedicated api endpoint([d52c308](https://github.com/ofkm/arcane/commit/d52c30820607b61cb3b0310635331e4c847629fc) by @kmendell)
- refactor and simplify cookies([c70523a](https://github.com/ofkm/arcane/commit/c70523aa346a9c50010d1d30d31e5794a75e995b) by @kmendell)
- cleanup auth middleware, refactor logging([09431a3](https://github.com/ofkm/arcane/commit/09431a3140ff5eefda387ec5c27e014f0afd7fb2) by @kmendell)
- cleanup old settings keys([9b65e72](https://github.com/ofkm/arcane/commit/9b65e72f2ecb051c6e4005b6a1a43505beed808e) by @kmendell)
- translate files([d75a3fc](https://github.com/ofkm/arcane/commit/d75a3fcf684a235f966ad8e4a381c9a4d6f4f456) by @kmendell)
- build frontend for svelte check([ce1b035](https://github.com/ofkm/arcane/commit/ce1b035ef762486b4b4e349041bebb6bd365aac3) by @kmendell)
- fix translations key([167225d](https://github.com/ofkm/arcane/commit/167225d37eb771fad1b4370caae88fabd7c40e9b) by @kmendell)
- add crowdin file and fix tests([fb1008c](https://github.com/ofkm/arcane/commit/fb1008cdee36dc2436afcc344e730ec87f2f6200) by @kmendell)
- add ci skip tag to config([fa23075](https://github.com/ofkm/arcane/commit/fa2307530beb73c48974c5dd5920f28092f15332) by @kmendell)
- update repo resources([7e58de2](https://github.com/ofkm/arcane/commit/7e58de2d4b57368cd5fdcfbaa162dd10b5cd5547) by @kmendell)
- update env example([3181371](https://github.com/ofkm/arcane/commit/318137195294a54e5b2f2d1443b6f8751960217b) by @kmendell)
- fix workflows runs([c50108c](https://github.com/ofkm/arcane/commit/c50108c5e85b194531642d57e67912f6bc8e74d5) by @kmendell)
- add Nederlands files([87ce674](https://github.com/ofkm/arcane/commit/87ce67446c66df18334ee7b467cf320b68df21a1) by @kmendell)
- refactor test utilities([69ba1d0](https://github.com/ofkm/arcane/commit/69ba1d02350712b80baf943c73aa151d417b478d) by @kmendell)
- add container spec([136230d](https://github.com/ofkm/arcane/commit/136230d5d14cd6ee87de554c0c4430d0a06967c1) by @kmendell)
- add registry spec([87b3604](https://github.com/ofkm/arcane/commit/87b36042ac56d6b41258a67aa10f827054fdfc28) by @kmendell)
- add 1.0 feedback issue template([67f5a8a](https://github.com/ofkm/arcane/commit/67f5a8a9f7e5c905a33ca75c538de1576342598e) by @kmendell)
- prepare for merge([c9fe3bd](https://github.com/ofkm/arcane/commit/c9fe3bdb144be3c09bae4e22543582831f6df6ac) by @kmendell)
- update release scripts([6fc67ca](https://github.com/ofkm/arcane/commit/6fc67ca71eb044cf9d4b57ad0553c77e5410907f) by @kmendell)
- fix next image pipefall([5e9528b](https://github.com/ofkm/arcane/commit/5e9528b0bb2732db658fab2355893ab0b35b05f5) by @kmendell)

**Full Changelog**: https://github.com/ofkm/arcane/compare/1.0-preview...v1.0.0

## 1.0-preview

### New features

- zod v4 form validation, and sheet based forms ([#301](https://github.com/ofkm/arcane/pull/301) by @kmendell)
- remote environments ([#305](https://github.com/ofkm/arcane/pull/305) by @kmendell)
- redesign login page([8063f08](https://github.com/ofkm/arcane/commit/8063f08bbcfc6caf8642185f70767fb607f1f157) by @kmendell)
- events are now shown in the ui ([#355](https://github.com/ofkm/arcane/pull/355) by @kmendell)
- migrate logging to json([b66afe2](https://github.com/ofkm/arcane/commit/b66afe258028c373f18f126403b70ce719d8efa9) by @kmendell)
- migrate logging to json([6ea6c77](https://github.com/ofkm/arcane/commit/6ea6c77dc55faa629919ce626e1c6e5cf08ee07e) by @kmendell)
- change port to 3552, cleanup old code([9705277](https://github.com/ofkm/arcane/commit/970527767b11b8eca774e68e0c64de3bd2e97d9e) by @kmendell)
- add copier utility functions for struct mapping([477c2d5](https://github.com/ofkm/arcane/commit/477c2d5d7e9a7635026ba305e15e643fc970870c) by @kmendell)
- show error status for image updates, show if a credential was used([7718605](https://github.com/ofkm/arcane/commit/7718605ea7386d7eba281b5ef98d3c1e18b33417) by @kmendell)
- add event details dialog([d14609c](https://github.com/ofkm/arcane/commit/d14609c4535ddc87a09385ead4acdcc0ccf920fb) by @kmendell)
- enhance volume details with container names and IDs([1c5cb35](https://github.com/ofkm/arcane/commit/1c5cb359d4f54612fd5107d6a1643a74e01304ad) by @kmendell)

### Bug fixes

- mismtached json type for database([44dbd27](https://github.com/ofkm/arcane/commit/44dbd27b00810d98e58cdad30961d8581fa4c1ca) by @kmendell)
- stack logic ([#313](https://github.com/ofkm/arcane/pull/313) by @kmendell)
- cleanup hooks and layout logic and disable SSR so local development works([e3750e5](https://github.com/ofkm/arcane/commit/e3750e5359e5faeab0bedbfa850481b26d8f7d1d) by @kmendell)
- image maturity/update logic([92ed4a0](https://github.com/ofkm/arcane/commit/92ed4a02937fc01448c56489981c4d017062e685) by @kmendell)
- switch from bcrypt to argon2 for password hashing([1485a53](https://github.com/ofkm/arcane/commit/1485a53044e7b0f1bb8c159fe1dcd45e7836f763) by @kmendell)
- lazy load all resources([066eadc](https://github.com/ofkm/arcane/commit/066eadcd64ff3c44f3ae76693c338a969fe55952) by @kmendell)
- template loading and usage([c994404](https://github.com/ofkm/arcane/commit/c994404d48c7a8ab7eabc88128656ea4bcf207f1) by @kmendell)
- update system meters dynamically([16e41cf](https://github.com/ofkm/arcane/commit/16e41cfcfb483f78f45b666f7f1e064346bf5b26) by @kmendell)
- add more options for onboarding ([#321](https://github.com/ofkm/arcane/pull/321) by @kmendell)
- auto update service logic ([#323](https://github.com/ofkm/arcane/pull/323) by @kmendell)
- incorrect user reponse structure and missing types([4622ca6](https://github.com/ofkm/arcane/commit/4622ca6b6c9b137dc9fe4d3cc3c5802f1a9f7703) by @kmendell)
- layout shift when using dropdown menus([e404eee](https://github.com/ofkm/arcane/commit/e404eee5f4179727ee31054b2559732825e17066) by @kmendell)
- show create container sheet([aa089c5](https://github.com/ofkm/arcane/commit/aa089c5b7b074f4510e6cb9421a4d8bbda3339ff) by @kmendell)
- simplify sqlite string, and make sure postgres migrations work([1a01113](https://github.com/ofkm/arcane/commit/1a01113aa77cd57a5c4d71fd2c159de0460c4373) by @kmendell)
- use containerId for removing containers([20cf8c9](https://github.com/ofkm/arcane/commit/20cf8c94701a70cf3f7f0a0f6f981cde72324333) by @kmendell)
- truncate volume name text([5111195](https://github.com/ofkm/arcane/commit/5111195a7f3eacd4aad9dfc246b948da8425f2f4) by @kmendell)
- onboarding oidc config([cadda0a](https://github.com/ofkm/arcane/commit/cadda0aa7a8bc1f77bce7b8997af58e08b113ded) by @kmendell)
- image dropdown not deleting images([12a2e8d](https://github.com/ofkm/arcane/commit/12a2e8d56b3a459627a8f1e78b848679d92e0539) by @kmendell)
- only show floating header after scrolling on compose page([6582e1e](https://github.com/ofkm/arcane/commit/6582e1e036b37f05a85d48d6eadb1a742a06aba8) by @kmendell)
- remove unused prop([3d8a350](https://github.com/ofkm/arcane/commit/3d8a350812e6829dadc6e03fa55e6d56e0180d1b) by @kmendell)
- use correct redirect after login([58a4340](https://github.com/ofkm/arcane/commit/58a43404ef3bde179cd86b3568bfb1be3105493e) by @kmendell)
- add loading indicators to metric cards, fix some styling on the dashboard([d9be7cc](https://github.com/ofkm/arcane/commit/d9be7cc5d809856c13fbf4f20819402aa7ce4ba3) by @kmendell)
- cleanup docker settings page ui([4874a48](https://github.com/ofkm/arcane/commit/4874a48c6bd23410ec1b8026742b3575fd028c8f) by @kmendell)
- use Arcane as page title for all pages([c1f4aed](https://github.com/ofkm/arcane/commit/c1f4aed593cadb980624072266a00eba5b7a6032) by @kmendell)
- only show edit user button for local users([a0eb2f4](https://github.com/ofkm/arcane/commit/a0eb2f421a99d018f8a347cd598f8b9f8e542790) by @kmendell)
- use correct destructive variant for user dropdown menu([84fb150](https://github.com/ofkm/arcane/commit/84fb1506b6bb7808ff96bf4c715b00d94fd051e7) by @kmendell)
- add missing props to button([044396c](https://github.com/ofkm/arcane/commit/044396c641cc3713976e0ed4b846470b2db53e0b) by @kmendell)
- revert redirect in layout([c659707](https://github.com/ofkm/arcane/commit/c65970710f16cbe2ae2547a474c64be4779ee08e) by @kmendell)
- make System username red on event table([0a8bff6](https://github.com/ofkm/arcane/commit/0a8bff6186246ea0643b6063db4cedff3d121013) by @kmendell)
- correct counts of containers and images on dashboard([520a18a](https://github.com/ofkm/arcane/commit/520a18a52a8e3ac437efa7b418804eb8ae96495a) by @kmendell)
- remove missing util file([23d7745](https://github.com/ofkm/arcane/commit/23d7745baf0f27a886169646e9413d6099b7525a) by @kmendell)
- remove oidc client secret from api reponses([4ab9a9d](https://github.com/ofkm/arcane/commit/4ab9a9d9ecbb98840a57a2c64c0e01ff8c0edd9b) by @kmendell)
- session validation timeouts([a267b91](https://github.com/ofkm/arcane/commit/a267b91506ec1cea1f7e6c983d05aa5826353cda) by @kmendell)
- optimize conversion of slog attributes for logging([10df97f](https://github.com/ofkm/arcane/commit/10df97f27b4a70c12b51ff3f186d28be6bdcc2b1) by @kmendell)
- settings not saving on submission([520178b](https://github.com/ofkm/arcane/commit/520178b28141ce697c63a12f4b92da64de573a95) by @kmendell)
- only allow role changes for oidc users([f5a3302](https://github.com/ofkm/arcane/commit/f5a3302f846b063e74ff6e10259e22c6fc331713) by @kmendell)
- status badge color is now reactive([8c2c24d](https://github.com/ofkm/arcane/commit/8c2c24df79179f00f127e697f913107a2ff7fd37) by @kmendell)
- remove ping group and utility in Dockerfile-static([c7ff050](https://github.com/ofkm/arcane/commit/c7ff050c56848807cf2133e8d7673d00a826ae58) by @kmendell)
- show x of x running for conatiner metric card([a03f336](https://github.com/ofkm/arcane/commit/a03f3365b3454343c125715d40de8649e2b9b260) by @kmendell)
- use dockerInfo for container and image counts([3413dc4](https://github.com/ofkm/arcane/commit/3413dc43192aa841d7b34178b889f2bc5e5baf68) by @kmendell)
- use dedicated endpoint for totalImageSize([58edb08](https://github.com/ofkm/arcane/commit/58edb08bdda3413ea07d2e3d018188d0917166cc) by @kmendell)
- return correct network reponse data on page load([b767fd5](https://github.com/ofkm/arcane/commit/b767fd577afcd1c123d42f3af019b214b7e1f980) by @kmendell)
- return correct event api reponse on page load([f9f3082](https://github.com/ofkm/arcane/commit/f9f3082667686225003eb7a73f92cb9bf8af0684) by @kmendell)
- use correct container data in api on page load([d33dedc](https://github.com/ofkm/arcane/commit/d33dedc77e2fc355b61238ced48f3f3492b56185) by @kmendell)
- use correct volume data in api on page load([c9bbffe](https://github.com/ofkm/arcane/commit/c9bbffe8ad141820ddfeb89e480ba0dd1c8ae8bf) by @kmendell)
- make onboardin security settings page the same as the normal settings page([20800ef](https://github.com/ofkm/arcane/commit/20800efb47ececf2d7ad32844dabc3bb491f27ce) by @kmendell)
- use correct image api reponse data([a08882a](https://github.com/ofkm/arcane/commit/a08882adc2fa46c081aa5574aa4e800b8494c277) by @kmendell)
- persist Docker image ID as primary key and improve tag selection logic([6e7ac85](https://github.com/ofkm/arcane/commit/6e7ac853da7f675f3ce1ce8867c447203b290035) by @kmendell)
- enhance container stats and configuration display with derived flags for environment variables, ports, labels, and network settings([3a232df](https://github.com/ofkm/arcane/commit/3a232df5f43a1dd3114f705742f62b2689aee715) by @kmendell)
- improve error handling and streaming logic in GetStatsStream and GetLogsStream methods([3a407d3](https://github.com/ofkm/arcane/commit/3a407d3da18ec0b04ca8a02680b96c2903249ac0) by @kmendell)
- add validation for stack ID and improve error handling in GetStackLogsStream method([57f25d2](https://github.com/ofkm/arcane/commit/57f25d2851e6762eb9416c4bd05434c9af0f42d6) by @kmendell)
- enhance login response to include token pair and set cookie for password change([587a656](https://github.com/ofkm/arcane/commit/587a656540d6de7f22b5a8679e60a893933fd07c) by @kmendell)
- make image update item reactive with status once clicked([fe5caf6](https://github.com/ofkm/arcane/commit/fe5caf639b6a35cf19b53d8a1c78fe1133fdba0d) by @kmendell)
- add user ID validation in token verification process([5595894](https://github.com/ofkm/arcane/commit/55958945c077eeabea17cf43694fb6256c2756c2) by @kmendell)
- refactor user creation logic to use CreateUser type and improve type safety([6ccb106](https://github.com/ofkm/arcane/commit/6ccb106795f9376d58ec04b1809e8ae5bebe138f) by @kmendell)
- projects page wont load if no stacks are found([320edc9](https://github.com/ofkm/arcane/commit/320edc9596ef382e49d58fcaa9949f5f5b4d37d7) by @kmendell)
- image table disappears when filtering images([12c6d9c](https://github.com/ofkm/arcane/commit/12c6d9c1d9bdff2348720bbe99a8a39eaa55077d) by @kmendell)
- pagination round one fixes (containers, images, and projects)([8349d1b](https://github.com/ofkm/arcane/commit/8349d1b84a85f2b112b54ab527e38fd390c18872) by @kmendell)
- stopAll button use the correct count([8bd2f5c](https://github.com/ofkm/arcane/commit/8bd2f5cec774fffe6bf612004b690dbcc878fe7c) by @kmendell)
- use new image detail type([63f8211](https://github.com/ofkm/arcane/commit/63f821186d105ab7f2aa17a37bb6f4ddf01a94af) by @kmendell)
- auth session redirect([9da620a](https://github.com/ofkm/arcane/commit/9da620a9534052d3aa0c9e26ca5c41cfe3cc7caa) by @kmendell)
- user display not showing in sidebar([e059cf2](https://github.com/ofkm/arcane/commit/e059cf2cb1ce78ea28e0a968d7690a67dd8ab594) by @kmendell)
- make sure oidc uses refresh tokens([9f185eb](https://github.com/ofkm/arcane/commit/9f185eba457f7f2ccf74ffa24753e6b3924efb50) by @kmendell)
- auth redirect (i hope)([92c1685](https://github.com/ofkm/arcane/commit/92c1685c238e87d19fb56a54c40f6c6b20f6b157) by @kmendell)
- sidebar shows on logon([6f14c05](https://github.com/ofkm/arcane/commit/6f14c050c899633d3e6feb83ff71c81fda3475b1) by @kmendell)
- redirect to /compose after project delete([ca8b4c6](https://github.com/ofkm/arcane/commit/ca8b4c6254f0da2a8d4f94a08ca3593be1988b87) by @kmendell)
- rework templates with new schema ([#389](https://github.com/ofkm/arcane/pull/389) by @kmendell)
- update link to template settings in dialog component([55b8fb5](https://github.com/ofkm/arcane/commit/55b8fb5fa4fcc85f4ad8969a7acf7666acc78a4a) by @kmendell)
- fix performance of projects page and rely only on filesystem watcher for updating the database([dee13bc](https://github.com/ofkm/arcane/commit/dee13bcb3eecea7ee939e817e4e39364a9e871b3) by @kmendell)
- add container force removal checkboxes([6e449b2](https://github.com/ofkm/arcane/commit/6e449b2af26161284b4fc548e9cb9fb8fea5b82d) by @kmendell)
- use prune mode behavior([751f7c7](https://github.com/ofkm/arcane/commit/751f7c74e3a3f70aa121b6a52a08f3ce37ec1801) by @kmendell)
- global prune not pruning volumes([6a55fa1](https://github.com/ofkm/arcane/commit/6a55fa1a054ca49b49fa479d1610961859460f7d) by @kmendell)
- do not allow auth method to be turned off is only one is enabled([b310334](https://github.com/ofkm/arcane/commit/b3103347455525623781722670a8dbb21d6d4820) by @kmendell)
- container details page not laoding([c5e5b51](https://github.com/ofkm/arcane/commit/c5e5b51756e246b990fea23556b7754206450a94) by @kmendell)
- move auto updater to just updater confirm working with projects([1382213](https://github.com/ofkm/arcane/commit/1382213820ce7a70d9ccac4ed0fea1b4b6d41bf8) by @kmendell)
- do not run auto update job on container start([97736be](https://github.com/ofkm/arcane/commit/97736bef8e624d00e69e928f34da99dcc5d1e115) by @kmendell)
- cleanup of orphaned image update records after applying updates([31155c5](https://github.com/ofkm/arcane/commit/31155c5ca8ae6f8d982f8c616bd0612fc2f75784) by @kmendell)
- add missing isAdmin prop([659933a](https://github.com/ofkm/arcane/commit/659933a342bb8063d78048ea767542d62d0a1423) by @kmendell)
- add exclude label for updater([123f575](https://github.com/ofkm/arcane/commit/123f5753ce23515cc8d2cd1e2e12965e221d0665) by @kmendell)
- use prefered username in oidc cliams([ee46e9a](https://github.com/ofkm/arcane/commit/ee46e9aabe84b96fa1e1e49b40a1ca1b441f4b73) by @kmendell)
- restore translucency to badges([1a93bdc](https://github.com/ofkm/arcane/commit/1a93bdc3f3bd8609b763177a1789398221c46209) by @kmendell)
- remove container registry field data on sheet reopen([66f26f5](https://github.com/ofkm/arcane/commit/66f26f5ea613e6e378909667609f3255fa8c34c2) by @kmendell)
- use correct cookie timeout value([8b247a5](https://github.com/ofkm/arcane/commit/8b247a5c1d5c4342ca716eb7d2f411ee68357eb2) by @kmendell)
- correctly use minute value for sessions timeout([3b3c9f4](https://github.com/ofkm/arcane/commit/3b3c9f4e24512adeed49033eca2cc4c440e4fc1c) by @kmendell)

### Documentation

- update development docs([a9e0037](https://github.com/ofkm/arcane/commit/a9e0037e616bcb36ff14bfcb1620e960e9d6b118) by @kmendell)
- update remote environment documentation([9e21050](https://github.com/ofkm/arcane/commit/9e21050043bbf372099f33d54ba639c299dcd593) by @kmendell)
- update docusaurus to 3.8.1([0df0b3a](https://github.com/ofkm/arcane/commit/0df0b3ab0de28f0c470a52ac62ecab1575699f1a) by @kmendell)
- switch to refined-cf-pages-action for deploying site([b24ca61](https://github.com/ofkm/arcane/commit/b24ca615766d55fabdf14810c4f5519794c58b1d) by @kmendell)
- update configuration docs([eaac425](https://github.com/ofkm/arcane/commit/eaac425feceed249af01a3a1f7ee861ac03a1dfc) by @kmendell)

### Other

- use user-store for storing user([c124cdd](https://github.com/ofkm/arcane/commit/c124cddf3468b97f7550fb03cf52a405c3ad24cb) by @kmendell)
- fix tests([88797db](https://github.com/ofkm/arcane/commit/88797db25b88e0d49129b7e3f1d4f371e28d3778) by @kmendell)
- add go linter([1b04d74](https://github.com/ofkm/arcane/commit/1b04d74c67be57c1e4fb9b6ae5cc6d04ee694dea) by @kmendell)
- exclude frontend([29221ea](https://github.com/ofkm/arcane/commit/29221eaa50db026c14bd21b95dcb49ab2aa15742) by @kmendell)
- add gomod to dependabot([e5502fd](https://github.com/ofkm/arcane/commit/e5502fd61fecceac1533a7f42f6ef9a7685d5635) by @kmendell)
- remove agent-dto from frontend([f34166b](https://github.com/ofkm/arcane/commit/f34166b8f0cf806a955b97ed11f2fe1f0ecd1265) by @kmendell)
- fix type errors ([#311](https://github.com/ofkm/arcane/pull/311) by @kmendell)
- fix doc upload path([3843c7f](https://github.com/ofkm/arcane/commit/3843c7f88491069868c04e13cc05fdc0a8705fd0) by @kmendell)
- merge two preview workflows([0591973](https://github.com/ofkm/arcane/commit/059197394e7ba7122bf96a1ee22412ccd7a3621d) by @kmendell)
- update build paths for prod deployment([7a1ccbd](https://github.com/ofkm/arcane/commit/7a1ccbd0c8e6b0dc8a110bf545f6d6bc0e68ffc0) by @kmendell)
- fix go lint issues ([#312](https://github.com/ofkm/arcane/pull/312) by @kmendell)
- fix some tests([3dad305](https://github.com/ofkm/arcane/commit/3dad3054ddb6d60b6eb8e5c694aa4356bc803cc4) by @kmendell)
- upgrade npm packages([c412f32](https://github.com/ofkm/arcane/commit/c412f328839a263cd40a83b3784d65305aa4be19) by @kmendell)
- upgrade npm packages([1a3201e](https://github.com/ofkm/arcane/commit/1a3201e8353fd42779375531d035c4cdc5af4f9c) by @kmendell)
- use correct converter service([3e01a42](https://github.com/ofkm/arcane/commit/3e01a4266b50a4583aa0c43999f0a129c7001c99) by @kmendell)
- remove old csrf([ce8166a](https://github.com/ofkm/arcane/commit/ce8166addf0286682db8c9094dda332026b40547) by @kmendell)
- enable hot reloading for backend([d0b8f67](https://github.com/ofkm/arcane/commit/d0b8f672f38e5ffaf0bcee8a86c7989b12ed51dc) by @kmendell)
- remove unused function([6bc8f0c](https://github.com/ofkm/arcane/commit/6bc8f0c531947fa5f8ad76b8c6fb618b619acc18) by @kmendell)
- fix error handling([e750ba0](https://github.com/ofkm/arcane/commit/e750ba04247364e1c68a3e0d580a9c3dc820bfa0) by @kmendell)
- improve error handling in DeployStack method([72c57dd](https://github.com/ofkm/arcane/commit/72c57dd5051456ffbf35de5fe1d91ddae7eb9e5a) by @kmendell)
- remove air .bin directory([68402db](https://github.com/ofkm/arcane/commit/68402db1503c0d805e8faba3a6230ac1a556ba8a) by @kmendell)
- improve ui/ux of lazy loading on dashboard([6ce5094](https://github.com/ofkm/arcane/commit/6ce50946b48dfc80e84a2ef383d539ad781a501b) by @kmendell)
- clean up some ui elements([9282252](https://github.com/ofkm/arcane/commit/9282252e599863b420b79e22bac985018b01ab60) by @kmendell)
- use Geist font([362c50c](https://github.com/ofkm/arcane/commit/362c50cce3c368282d3157c41157d6381d855ec5) by @kmendell)
- add font files([866525f](https://github.com/ofkm/arcane/commit/866525f7c5f5aa6fbaf4f3d5de3211532d1e5acf) by @kmendell)
- fix go linter issues([e1613b1](https://github.com/ofkm/arcane/commit/e1613b1346cbc765a937dd7f446103a15993a5ac) by @kmendell)
- fix svelte check([fd10ad0](https://github.com/ofkm/arcane/commit/fd10ad002d09e2286bdb35ab5fd0ebe269eb0980) by @kmendell)
- pagination models and logic ([#330](https://github.com/ofkm/arcane/pull/330) by @kmendell)
- image update indicator ([#340](https://github.com/ofkm/arcane/pull/340) by @kmendell)
- update dropdown menu component([cde041a](https://github.com/ofkm/arcane/commit/cde041a068531e08e908a92324c1d5a2c6f89df3) by @kmendell)
- fix some layout issues([7ef9edf](https://github.com/ofkm/arcane/commit/7ef9edff70f66612e971428e28638616301a7255) by @kmendell)
- cleanup metric cards([9528cf6](https://github.com/ofkm/arcane/commit/9528cf6f63d69d2c402da91d5500a5c3b3bda6d0) by @kmendell)
- relayout dashboard([bcee55a](https://github.com/ofkm/arcane/commit/bcee55af1a36cba30f32a8396121e7bdba0077f1) by @kmendell)
- use destructive variant for dropdowns([d5de0dd](https://github.com/ofkm/arcane/commit/d5de0ddcbe186539df403fee4b6eede7947a316d) by @kmendell)
- cleanup networks table([ee6cb4c](https://github.com/ofkm/arcane/commit/ee6cb4ca93d06d8cb8ad137f588a13ef24a0fae9) by @kmendell)
- add back link for images([133866f](https://github.com/ofkm/arcane/commit/133866fa83326115750727778532bd4ec180ceef) by @kmendell)
- stop website deployments on push([f1b0691](https://github.com/ofkm/arcane/commit/f1b0691d563573fe12c0311f3a0afe8e2a5c37d6) by @kmendell)
- use capital letter for state text([c48106f](https://github.com/ofkm/arcane/commit/c48106fd88cda48e25760c3a47108ddde8457fa6) by @kmendell)
- fix incorrect docker type([5b4488a](https://github.com/ofkm/arcane/commit/5b4488af7b656d52a2883734cee017af3c507641) by @kmendell)
- improve compose states and importing logic([8a11c76](https://github.com/ofkm/arcane/commit/8a11c764be0ccd456390d4f74b86cd09b2c43caa) by @kmendell)
- cleanup some repo files([82ac390](https://github.com/ofkm/arcane/commit/82ac390defad278db6eaa7263726b93ff6ceaa8d) by @kmendell)
- fix lockfile([b900a77](https://github.com/ofkm/arcane/commit/b900a776ce0fe6d5b0f6bb08b05e9a4bb2bbafac) by @kmendell)
- remove unused import([faa561d](https://github.com/ofkm/arcane/commit/faa561de8c4e6063ed611e92a9c43275a8a2d7f3) by @kmendell)
- add backend logic for playwright tests and fix exsisting tests ([#350](https://github.com/ofkm/arcane/pull/350) by @kmendell)
- add docker build tag for e2e tests([933668d](https://github.com/ofkm/arcane/commit/933668df6521dec8b2a830cf97e61ad97967a075) by @kmendell)
- use npm install over npm ci([c9e0366](https://github.com/ofkm/arcane/commit/c9e036688e69979bb72bac93f4be974199a289e4) by @kmendell)
- cleanup e2e tests workflow([92a6726](https://github.com/ofkm/arcane/commit/92a67263879fd1446b059fbbd24b7950f08c4cfd) by @kmendell)
- remove --build from tests([28fc996](https://github.com/ofkm/arcane/commit/28fc996972375870257270c6d9644c9f78bffdab) by @kmendell)
- fix go lint issues([fc90831](https://github.com/ofkm/arcane/commit/fc9083123f0259836218bbb9c18981c038e1a231) by @kmendell)
- update dependabot path([52022e6](https://github.com/ofkm/arcane/commit/52022e605c79c65ccf1064dd540b254f56071400) by @kmendell)
- use oidc well-known to discover urls ([#351](https://github.com/ofkm/arcane/pull/351) by @kmendell)
- cleanup github assets, and remove docs and doc workflows([00be577](https://github.com/ofkm/arcane/commit/00be57785d1c7d8133a0f0ad89e41dce95610e02) by @kmendell)
- update readme with development note ([6aa482a](https://github.com/ofkm/arcane/commit/6aa482a1a82d3a6938e0e169830eeb58221e3d01) by @kmendell)
- add preview workflow([3f9450a](https://github.com/ofkm/arcane/commit/3f9450ac5d4f50c30055b4e1a77cdeff86af84bc) by @kmendell)
- add qemu to preview workflow([285fea8](https://github.com/ofkm/arcane/commit/285fea85a6ed943b8d4548e73af32dbc20bc0009) by @kmendell)
- update preview workflow([74685ca](https://github.com/ofkm/arcane/commit/74685cae9dcf1cd90f4d50b2d5adaba76d611d6a) by @kmendell)
- update preview workflow to use 1.0-preview([63d7dec](https://github.com/ofkm/arcane/commit/63d7deccd6ac2a100d3cb1023c6295ca2c1bde50) by @kmendell)
- auto update now uses the new image_update_service([550ae59](https://github.com/ofkm/arcane/commit/550ae59512eecddb049e91d9a30d6cbab94001f2) by @kmendell)
- switch to sql migrations, and key value store for settings ([#353](https://github.com/ofkm/arcane/pull/353) by @kmendell)
- fix svelte check([4103356](https://github.com/ofkm/arcane/commit/41033566e9cec74a5a7dde4d7a1d6d1e915d4de4) by @kmendell)
- fix go linter([4948dde](https://github.com/ofkm/arcane/commit/4948dde46b954cc426faabeb9ececb71ac393729) by @kmendell)
- frontend stack to project migration([9809d51](https://github.com/ofkm/arcane/commit/9809d51c7421f19765f2b2134c31ab47254bfa2d) by @kmendell)
- add project pages tests([31fcedd](https://github.com/ofkm/arcane/commit/31fcedd69b4933ef44aca375e8e563743b5759b5) by @kmendell)
- add a static projects folder for tests([0dd2a2e](https://github.com/ofkm/arcane/commit/0dd2a2e1024b47ba95ad59093349066687cef135) by @kmendell)
- fix fk migrations([e8954c0](https://github.com/ofkm/arcane/commit/e8954c0967f65bf27f52adddb51d6f99cd6a0d93) by @kmendell)
- add missing cases to event service([651e8f3](https://github.com/ofkm/arcane/commit/651e8f3c4b3243315f9135b0d33f553334ad94eb) by @kmendell)
- remove selected labels([6e3fd3e](https://github.com/ofkm/arcane/commit/6e3fd3eca11a28dc62ba73c69258b6ad6ea20c5e) by @kmendell)
- fix e2e tests([c0ca6c3](https://github.com/ofkm/arcane/commit/c0ca6c32bbcf8a0524f4fc6c945f1fafc34e83aa) by @kmendell)
- fix the layout of some pages([8c95616](https://github.com/ofkm/arcane/commit/8c95616e526caaa2e1af0d50af354db662d42f69) by @kmendell)
- add updated error page([708bf42](https://github.com/ofkm/arcane/commit/708bf4240e66f40eee03a660f0af3aeb0db920af) by @kmendell)
- restyle toast prompts([4441989](https://github.com/ofkm/arcane/commit/44419897bc5f9b3d5736989d0a53b6382f79b178) by @kmendell)
- redesign loading indicator([601a64e](https://github.com/ofkm/arcane/commit/601a64e7682c8f4b8d0b7a50c8bd388aa7e2257e) by @kmendell)
- fix some minor issues([ff95d7e](https://github.com/ofkm/arcane/commit/ff95d7ec20aa2e998b6046510ae8ee590eb197ca) by @kmendell)
- update to go 1.25([c0a5d94](https://github.com/ofkm/arcane/commit/c0a5d94370dc2109216a5e924ba3b236e101c9ef) by @kmendell)
- update to go 1.25([b02cbf0](https://github.com/ofkm/arcane/commit/b02cbf09cdd26a94cd391a0162525e8de9c846d4) by @kmendell)
- update checkout action to v5 and improve cache dependency path([4709771](https://github.com/ofkm/arcane/commit/4709771d25b3a65d8c9a2500a32f4a7864c4ab5a) by @kmendell)
- improve stack listing by detaching cache update context and removing unused fields([2bfef50](https://github.com/ofkm/arcane/commit/2bfef50afc8baef3b455abf23e599e4bc7a4a14a) by @kmendell)
- use uuid as ID in base model([082d4ef](https://github.com/ofkm/arcane/commit/082d4eff58f4d2c54812cc3e4d24a81f46c77103) by @kmendell)
- remove image size from dashboard([a0f8def](https://github.com/ofkm/arcane/commit/a0f8defe6dd987c76918a020d6a31aa8a89cf92a) by @kmendell)
- fix linter([bc56777](https://github.com/ofkm/arcane/commit/bc567772c0ce2cbbdc956face95de58eb636ddbc) by @kmendell)
- test new svelte check workflow([b869d3f](https://github.com/ofkm/arcane/commit/b869d3f9a8c83b5f399b3e9102925d54080ed685) by @kmendell)
- test new svelte check workflow([0532a94](https://github.com/ofkm/arcane/commit/0532a94a7cd9965486c8f4a43a6773d0a2747a4e) by @kmendell)
- revert svelte check workflow([27f4154](https://github.com/ofkm/arcane/commit/27f4154394d8e3d5702df98f4bc4cee354bc96e3) by @kmendell)
- dont lint update settings([cdfc209](https://github.com/ofkm/arcane/commit/cdfc209a731a672d10646feb829845fda89b4f71) by @kmendell)
- update tests to port 3552([8c6baa4](https://github.com/ofkm/arcane/commit/8c6baa4956d36c1bb90440421ec34d28a1cb6f55) by @kmendell)
- update remaining 8080 ports to 3552([97af662](https://github.com/ofkm/arcane/commit/97af66248b22e833c2c709cdd3306633b9d40f88) by @kmendell)
- switch to pnpm and add release workflows([25ddecd](https://github.com/ofkm/arcane/commit/25ddecdd07757669a1f29adb7aa7fc6cb8b4c061) by @kmendell)
- update dockerfile with pnpm([6298cc5](https://github.com/ofkm/arcane/commit/6298cc52ce73d1815924ae1658751d756344936a) by @kmendell)
- simplify docker file([0b9a7f5](https://github.com/ofkm/arcane/commit/0b9a7f5fd6cafdf16f97ab41aee0045a21d24013) by @kmendell)
- add required deps and restructure docker file([9b93ea3](https://github.com/ofkm/arcane/commit/9b93ea385809cbaeaf657c76dc4b3bfd87e40554) by @kmendell)
- update @codemirror/language dependency to version 6.11.3([0f68dea](https://github.com/ofkm/arcane/commit/0f68deaf4353e76f9d393b65f211a12ce8e7f34f) by @kmendell)
- add missing @codemirror/lint and @codemirror/state dependencies([62e8ba1](https://github.com/ofkm/arcane/commit/62e8ba1a8d97d8b2e114ccd37e6e290e8eaaf4a8) by @kmendell)
- add 1.0 preview release workflow([2eb1448](https://github.com/ofkm/arcane/commit/2eb1448fea46fa6e44885e56a6e39c041d98db1a) by @kmendell)
- use static for 1.0 preview([7399b28](https://github.com/ofkm/arcane/commit/7399b2808f8ebf49e1c40663f31b0657e2165c91) by @kmendell)
- update Dockerfile and migration logic for SQLite, add new dependencies([f723ed1](https://github.com/ofkm/arcane/commit/f723ed1d4ad19b3571a87f6dd5c07cdc7ac5e52c) by @kmendell)
- add correct packages to Dockerfile-static([94a85df](https://github.com/ofkm/arcane/commit/94a85df13bb501ff807223385ff595642ac0c2e2) by @kmendell)
- add timestamp to preview release([6a60a67](https://github.com/ofkm/arcane/commit/6a60a6774985c5f2a6a37c2afcdc85e8aa9ceb5a) by @kmendell)
- move the 1.0-preview tag to latest commit([95cd7c6](https://github.com/ofkm/arcane/commit/95cd7c6691bc1d2373a962e8b175d66ded60af44) by @kmendell)
- remove advanced codeql([7e5196c](https://github.com/ofkm/arcane/commit/7e5196ced35f558d13a948b2bb5d1382a7696ccd) by @kmendell)
- fix svelte check([3e073e8](https://github.com/ofkm/arcane/commit/3e073e825f87ab9d7c5bc2348df519798176b721) by @kmendell)
- cleanup([2b713ea](https://github.com/ofkm/arcane/commit/2b713ea9a5306d1e4cc4b055415ac54af0e6038e) by @kmendell)
- cleanup image,stack, and network services([66084a4](https://github.com/ofkm/arcane/commit/66084a40dddf8ca0297c1f248c9571f1bead8540) by @kmendell)
- cleanup more backend services and restructure dtos([a66ea02](https://github.com/ofkm/arcane/commit/a66ea025f2a1b4d74aab22dc0ac8fcb071afce88) by @kmendell)
- models and services to remove user session handling([feb9dfb](https://github.com/ofkm/arcane/commit/feb9dfbd950626cc9e35a8041340478322c08e2f) by @kmendell)
- add backend unit tests and implement registry utility tests([9e6bdfc](https://github.com/ofkm/arcane/commit/9e6bdfc2f2d7268bb3191290e5da7ba823a2e304) by @kmendell)
- update backend tests workflow to include GCC installation and enable race detection([ae7e660](https://github.com/ofkm/arcane/commit/ae7e6605813d9271a270918e3bf7214b375c6a95) by @kmendell)
- add build tags to unit tests([18db647](https://github.com/ofkm/arcane/commit/18db647a84526fa3e61b9f8a0d4462264cc1d01d) by @kmendell)
- fix linter([5e097b4](https://github.com/ofkm/arcane/commit/5e097b43cf7dc2947647258031948b2de5ff4cfb) by @kmendell)
- improve error handling and response validation in image fetching logic([a434c6e](https://github.com/ofkm/arcane/commit/a434c6e8633bbf2f1accb1cb82dbb08bc2c29c9e) by @kmendell)
- update tables names to remove \_table([7a2d823](https://github.com/ofkm/arcane/commit/7a2d823d076f6d23493ca4df2638519aca56c597) by @kmendell)
- add both sqlite and postgres e2e tests([c2a39c8](https://github.com/ofkm/arcane/commit/c2a39c8099cf84a8cf222d415b000461f5de2cae) by @kmendell)
- remove comments([6fa04e6](https://github.com/ofkm/arcane/commit/6fa04e6e847e6de19413117abe4ad9498a348d08) by @kmendell)
- add fs watcher utility to sync the stacks when a change is made on the filesystem([f465d36](https://github.com/ofkm/arcane/commit/f465d3645e0d3a7c3e6693a33828557a83bdbccc) by @kmendell)
- cleanup on some auth services([c724e47](https://github.com/ofkm/arcane/commit/c724e47aa5e75944da52783789f8e64dbd8afa4f) by @kmendell)
- move some helpers to utils([3b51b65](https://github.com/ofkm/arcane/commit/3b51b65de1c437c3ae759fbb35f76a987a91ab1b) by @kmendell)
- fix linter([2a8cd2a](https://github.com/ofkm/arcane/commit/2a8cd2abe77dbfb2b849a230ec823e80ff344bac) by @kmendell)
- remove and refactor some tests([b58c9ce](https://github.com/ofkm/arcane/commit/b58c9ce5232760d517942f782e787059aea09cb3) by @kmendell)
- fix test selectors([aab7202](https://github.com/ofkm/arcane/commit/aab72022da59fa87fc161d7b41f4a4c889f93ed5) by @kmendell)
- remove lockfile install for tests([13ce35f](https://github.com/ofkm/arcane/commit/13ce35f29a9dd64b3b65c550fc6ea80fc999b8f6) by @kmendell)
- test new svelte check workflow([02cd170](https://github.com/ofkm/arcane/commit/02cd1700238fdd0db2df041dd1120a0d4ff4d71e) by @kmendell)
- network and volume handling in frontend and backend([6f2442a](https://github.com/ofkm/arcane/commit/6f2442ad26633c64f0cd49ea3e32ae01efd61d60) by @kmendell)
- fix linter([ce1b2de](https://github.com/ofkm/arcane/commit/ce1b2de4cf7ed36d3b66ece16ac1da18cbf7e1d1) by @kmendell)
- fix volume tests([a813667](https://github.com/ofkm/arcane/commit/a81366741d3c4254d699c5ec9619ada3673b6ecb) by @kmendell)
- add network.spec([11b3c30](https://github.com/ofkm/arcane/commit/11b3c306c5ab5760c08c560a4cbdfa1b575a582a) by @kmendell)
- fix go linter([e362bad](https://github.com/ofkm/arcane/commit/e362badab0678f6dabcff6b288fff04af7a255ea) by @kmendell)
- recreate arcane table using tanstack table ([#390](https://github.com/ofkm/arcane/pull/390) by @kmendell)
- fix go linter([6e8f2f3](https://github.com/ofkm/arcane/commit/6e8f2f37e8b945a1a6c819987062cd8b99ff5bfd) by @kmendell)
- enhance encryption initialization and key handling([e2e8ddb](https://github.com/ofkm/arcane/commit/e2e8ddbe13d720f4a12321bba3b467466c4ba765) by @kmendell)
- cleanup logging([2a3fb4e](https://github.com/ofkm/arcane/commit/2a3fb4ed0b397acd508cd1d14a7f11256b6c7b2e) by @kmendell)
- update SettingVariable methods to use value receiver and add recvcheck linter([6e5ab7f](https://github.com/ofkm/arcane/commit/6e5ab7fbb2e12bafe3d8845477adb6d19b6ec64d) by @kmendell)
- project creation and edit form validation using zod([ee807fe](https://github.com/ofkm/arcane/commit/ee807feb2287a869ae8bfd741f7e5d8792c8f415) by @kmendell)
- add image pull popover when pulling projects([2edc661](https://github.com/ofkm/arcane/commit/2edc661020bc6135d3354eb11d4d0dd62627af48) by @kmendell)
- add arrow to popover([2923311](https://github.com/ofkm/arcane/commit/292331116f53e1ae657f2ac717b30d17a3d6c354) by @kmendell)
- move progress popover to its own component([c83995d](https://github.com/ofkm/arcane/commit/c83995d70cd3c5ca3e7bbee62f3b65d627babb7c) by @kmendell)
- improve log viewer performance([b53c3fc](https://github.com/ofkm/arcane/commit/b53c3fc4bdc8d93f0a6bb92a91e84d7c67e246b3) by @kmendell)
- redesign dashboard([41a4b8b](https://github.com/ofkm/arcane/commit/41a4b8b5de6dd785625ee530283fdd2cb2feda11) by @kmendell)
- code editor rework ([#396](https://github.com/ofkm/arcane/pull/396) by @kmendell)
- update lockfile([2a7b582](https://github.com/ofkm/arcane/commit/2a7b582332eff199516d75d47b2dcaa94c0e267e) by @kmendell)
- update tests to use new dropdown button([586cf29](https://github.com/ofkm/arcane/commit/586cf29cf21d7d06921aed278339f4ae0aacbde6) by @kmendell)
- remove old unused test([0858131](https://github.com/ofkm/arcane/commit/085813153953d76695f7349e223d5711b84c374f) by @kmendell)
- always pull latest test images([bc13ca2](https://github.com/ofkm/arcane/commit/bc13ca243fb842852bfe2e53883e5d63687ca965) by @kmendell)
- fix lints([94178d6](https://github.com/ofkm/arcane/commit/94178d622f8d5c6ad5a127a642edaa425b2abcef) by @kmendell)
- cleanup unused routes([ba75009](https://github.com/ofkm/arcane/commit/ba75009d3c1df8e9a3109f13ad0157eec1a0a072) by @kmendell)
- use zod schemas on settings pages ([#397](https://github.com/ofkm/arcane/pull/397) by @kmendell)
- update lockfile([f1fbfd8](https://github.com/ofkm/arcane/commit/f1fbfd839e3f8c879617f33bfda37a7f92720bc1) by @kmendell)
- cleanup app.css([017c387](https://github.com/ofkm/arcane/commit/017c387db934f31ce214f2acdaa2587079f63096) by @kmendell)
- return error([b8c8b1b](https://github.com/ofkm/arcane/commit/b8c8b1bdb9ccb85fbdad6f1af6758fa663b6ea3f) by @kmendell)
- run formatter([f0390cd](https://github.com/ofkm/arcane/commit/f0390cd66a9bfbf0b94524b331890b376de4b19b) by @kmendell)
- add gravatar enabled setting([e2c88af](https://github.com/ofkm/arcane/commit/e2c88aff0ab36897115f44b0ceba1a07a86af0a1) by @kmendell)
- fix linter([5140cb2](https://github.com/ofkm/arcane/commit/5140cb23b381e0f884f8d8078641b75b03f5c8bb) by @kmendell)
- simplify response handling in image update endpoints([6add097](https://github.com/ofkm/arcane/commit/6add0973ea3a145baf6cc4975fbe1291ae556c14) by @kmendell)
- migrate logging to slog package and enhance logger configuration([2e69fe1](https://github.com/ofkm/arcane/commit/2e69fe16ad6e8e4d8f8105cea9ddaff18112dab9) by @kmendell)
- refactor api route structure, update auth middleware to actually pass if a user is an admin or not([4b0868e](https://github.com/ofkm/arcane/commit/4b0868e1ba8819d5b22832a17e31244617f203d1) by @kmendell)
- role mappings for OIDC users([7e7f8c0](https://github.com/ofkm/arcane/commit/7e7f8c0f82d6a9b2c4e23047c139b4405cc42957) by @kmendell)
- add environment to compose files([ffde804](https://github.com/ofkm/arcane/commit/ffde8044c50407db565ce583c574466540294dd5) by @kmendell)
- fix linter, refactor admin claim logic([a97097a](https://github.com/ofkm/arcane/commit/a97097a291df7f58a856700404878043397f0cfc) by @kmendell)
- refactor oidc status config([3c2d54d](https://github.com/ofkm/arcane/commit/3c2d54d78c60729070e66f84747d55b943e9ed78) by @kmendell)
- revert cookie time([94f0206](https://github.com/ofkm/arcane/commit/94f0206419c87fc646e5d91c572bd24c4c878082) by @kmendell)
- add image polling job([51e365e](https://github.com/ofkm/arcane/commit/51e365ebf4a2d1e47c912784fb08f885c212ba3a) by @kmendell)
- remote environments with access tokens ([#401](https://github.com/ofkm/arcane/pull/401) by @kmendell)
- update dockerfile location for e2e tests([3501f1e](https://github.com/ofkm/arcane/commit/3501f1ed29aefa55343d472bf9f79776200b5a58) by @kmendell)

**Full Changelog**: https://github.com/ofkm/arcane/compare/v0.15.1...1.0-preview

## v0.15.1

### New features

- migrate to Go backend, serve frontend from backend ([#291](https://github.com/ofkm/arcane/pull/291) by @kmendell)

### Bug fixes

- proper compose validation ([#272](https://github.com/ofkm/arcane/pull/272) by @kmendell)
- remove oidc env and args from docker build([e462e8b](https://github.com/ofkm/arcane/commit/e462e8bc271cf92f5b93c09c3d4fe8e87f5018bf) by @kmendell)
- compose network race condition ([#274](https://github.com/ofkm/arcane/pull/274) by @kmendell)
- layout of editors in compose details view([c052902](https://github.com/ofkm/arcane/commit/c05290291f556ac3e45c3479fe6ff2d3c72db6da) by @kmendell)

### Other

- compose spec type ([#275](https://github.com/ofkm/arcane/pull/275) by @kmendell)
- add dtos for most handlers([27d2500](https://github.com/ofkm/arcane/commit/27d25001731113f54ee6ab9252ee6118193cb729) by @kmendell)
- rework auto update service, and auto search registries ([#296](https://github.com/ofkm/arcane/pull/296) by @kmendell)
- remove docker host setting and only use local socket([f352383](https://github.com/ofkm/arcane/commit/f352383c911ba01b7d1bb0d085919f2450e17ff2) by @kmendell)
- fix some ui issues([5360bd6](https://github.com/ofkm/arcane/commit/5360bd619c3410992a39508c04f21e98a3fb5d9e) by @kmendell)
- auto search through private registries([a65f537](https://github.com/ofkm/arcane/commit/a65f537d585332efd80b9ba1e12bcb20fa3fde94) by @kmendell)
- fix playwright e2e tests ([#298](https://github.com/ofkm/arcane/pull/298) by @kmendell)
- make sure to copy dist folder([b71b549](https://github.com/ofkm/arcane/commit/b71b5495f6dc0d6da370e7426e715c76b7ef1bff) by @kmendell)
- restructure dockerfile([94d4653](https://github.com/ofkm/arcane/commit/94d46533e506091b0eb223bc390bcc526c43a907) by @kmendell)
- restructure dockerfile([bc15668](https://github.com/ofkm/arcane/commit/bc156687410f1c38677b63752904e8385599aa07) by @kmendell)
- fix docker compose syntax([ae4d89d](https://github.com/ofkm/arcane/commit/ae4d89d9c3fb9650cfb835852ca9229e40a04346) by @kmendell)
- fix type errors([bfd417a](https://github.com/ofkm/arcane/commit/bfd417a8adc6ea5d07b34453db740a6c4785011c) by @kmendell)

**Full Changelog**: https://github.com/ofkm/arcane/compare/v0.15.0...v0.15.1

## v0.15.0

### New features

- remote agents ([82cbab5](https://github.com/ofkm/arcane/commit/82cbab506083d6820611b68c3bafea36299a4f7c) by @kmendell)
- use drizzle as a database backend ([54061d3](https://github.com/ofkm/arcane/commit/54061d303945871bd998c3acf4cb331ce6eee560) by @kmendell)
- system usage meters([64dde28](https://github.com/ofkm/arcane/commit/64dde28ef34d43ce2bc5fa3390d2db9e013098e0) by @kmendell)

### Bug fixes

- container status sorting incorrect ([64faad3](https://github.com/ofkm/arcane/commit/64faad3479b541b04addb8ac2d31e0148061ea0a) by @kmendell)
- remove duplicate agent sidebar item([6545794](https://github.com/ofkm/arcane/commit/6545794c47f11566803a1c8772169919471dc439) by @kmendell)
- rename stacks to compose projects ([b38b298](https://github.com/ofkm/arcane/commit/b38b29851882d4c3dd91b4385f1ad1fb30035b8c) by @kmendell)
- deploy to agent dropdown button ([6092c4f](https://github.com/ofkm/arcane/commit/6092c4f67900f48ebc88d2463524669f8ad3c8f8) by @kmendell)
- volumes table truncate not being applied([ce50de7](https://github.com/ofkm/arcane/commit/ce50de7b96ad40fc7ae221517e86a8cea35b1625) by @kmendell)
- incorrect github link on login page([3205312](https://github.com/ofkm/arcane/commit/3205312997badc6e47069dae9ab422052aabb818) by @kmendell)
- container creation type mismatches not allowing containers to be created([e8aece6](https://github.com/ofkm/arcane/commit/e8aece65771c56d84360280835edc496130e88c9) by @kmendell)
- pruning button not showing loading status([419cd9e](https://github.com/ofkm/arcane/commit/419cd9ede5e905d4858b204e1228383ab8425037) by @kmendell)
- implement main compose spec functionality ([e2fc0ac](https://github.com/ofkm/arcane/commit/e2fc0ac7d53d27bb4c03f5bb50b059dd90faf1e4) by @kmendell)
- parse ipam config correctly([2585d69](https://github.com/ofkm/arcane/commit/2585d69e9ebb38fe44a0a71c4895455219a4c1a6) by @kmendell)
- agents not showing compose projects in table([0ab2757](https://github.com/ofkm/arcane/commit/0ab2757c47b0dba5d839d6853467febb503243f5) by @kmendell)
- use new template root url([55fdacd](https://github.com/ofkm/arcane/commit/55fdacd2bef3cebbba549fb90c2d27ae1ce2d4a8) by @kmendell)
- store image maturity in database instead of cache ([1b29808](https://github.com/ofkm/arcane/commit/1b298088172b959ea38b8ab624a24c6a3af3b65e) by @kmendell)
- cpu and ram usage bars not showing correct values([01fbb16](https://github.com/ofkm/arcane/commit/01fbb166675d1ff34e0a63516014140feecec598) by @kmendell)
- use system storage on dashboard([6e7a83c](https://github.com/ofkm/arcane/commit/6e7a83c8cb230f032adeefe8326c57da301f001e) by @kmendell)
- rework auto update service([d88178a](https://github.com/ofkm/arcane/commit/d88178a2c2c9edc308cbedccd80b83fbdc0ba2c7) by @kmendell)
- missing ) in migrations([635d932](https://github.com/ofkm/arcane/commit/635d932180395a0b8d2b1ec55f8ec0f0a382ab88) by @kmendell)
- support removing agents([c33a872](https://github.com/ofkm/arcane/commit/c33a872370ce82cc59839078f90448c6ce90daa1) by @kmendell)
- create database in docker build([24b33e8](https://github.com/ofkm/arcane/commit/24b33e8715faa0cff1d814df4427de8bd2d2415c) by @kmendell)

### Documentation

- update template docs with new root url([8c8a576](https://github.com/ofkm/arcane/commit/8c8a576c366c703114f3bdecb9ab25f118cef5ab) by @kmendell)
- add agent docs([a01dc39](https://github.com/ofkm/arcane/commit/a01dc397671cecc4a9024ee668113a68a8beb0d2) by @kmendell)
- add agent category([39d8201](https://github.com/ofkm/arcane/commit/39d8201e4e0707f8c7cf26e3e802796bfd1d00f4) by @kmendell)
- update agent docs([9971a9a](https://github.com/ofkm/arcane/commit/9971a9ab938a03834d7b5d4b87a085647d7b06ce) by @kmendell)
- update agent docs([419ce4d](https://github.com/ofkm/arcane/commit/419ce4d0b7e704be89916da4c77a6dc7f83cbd43) by @kmendell)
- add agent config docs([417a8b0](https://github.com/ofkm/arcane/commit/417a8b09e2f93d35137184b04633f24878f54326) by @kmendell)

### Dependencies

- bump tar-fs from 2.1.2 to 2.1.3 in the npm_and_yarn group ([#261](https://github.com/ofkm/arcane/pull/261) by @dependabot[bot])

### Other

- allow both envs([8521b7b](https://github.com/ofkm/arcane/commit/8521b7bf252d1d665e9cfa979fb9b43986b2b15e) by @kmendell)
- create db in test script([428b827](https://github.com/ofkm/arcane/commit/428b8277ede96b15f4e0902078fd82c35695c895) by @kmendell)
- fix test setup script([2a75cfb](https://github.com/ofkm/arcane/commit/2a75cfb3055672315c2d4f4be4a73b197df0a994) by @kmendell)
- fix migration logic([f523d41](https://github.com/ofkm/arcane/commit/f523d41331c28e2c60a1e0fe7c0507de032a2489) by @kmendell)
- add better static ip logging([327bb30](https://github.com/ofkm/arcane/commit/327bb3010f0dfd8bf233f0d06befe21933cecfe4) by @kmendell)
- remove stacks from agent headers([883a069](https://github.com/ofkm/arcane/commit/883a0696ca2b63e2751195be998380f9001bcae1) by @kmendell)
- code review fixes"([c374480](https://github.com/ofkm/arcane/commit/c374480c537c30bd0e14a632c2600978061fd4c6) by @kmendell)
- add missing agent types([#241](https://github.com/ofkm/arcane/pull/241) by @kmendell)
- move where db is created([d4fcda0](https://github.com/ofkm/arcane/commit/d4fcda04678a409538ba8393d7db0750221b4cec) by @kmendell)
- bump the dev-dependencies group with 2 updates ([#249](https://github.com/ofkm/arcane/pull/249) by @dependabot[bot])
- bump the prod-dependencies group across 1 directory with 10 updates ([#267](https://github.com/ofkm/arcane/pull/267) by @dependabot[bot])

**Full Changelog**: https://github.com/ofkm/arcane/compare/v0.14.0...v0.15.0

## v0.14.0

### New features

- compose and .env template ([#231](https://github.com/ofkm/arcane/pull/231) by @kmendell)

**Full Changelog**: https://github.com/ofkm/arcane/compare/v0.13.1...v0.14.0

## v0.13.1

### Bug fixes

- re-release 0.13.0 as 0.13.1([bc2e4a2](https://github.com/ofkm/arcane/commit/bc2e4a2edcfe225aee4df50e8d98b06e6b2de7c4) by @kmendell)

**Full Changelog**: https://github.com/ofkm/arcane/compare/v0.13.0...v0.13.1

## v0.13.0

### New features

- allow changing user usernames([74321b5](https://github.com/ofkm/arcane/commit/74321b52e891cb25c8d205caf4c451696fc3c200) by @kmendell)
- make compose editor widths resizeable([64c33e6](https://github.com/ofkm/arcane/commit/64c33e65c87146d9cedc5647b2840b3ca6623a13) by @kmendell)
- simplify container and satck detail pages ([#227](https://github.com/ofkm/arcane/pull/227) by @kmendell)

### Bug fixes

- theming not applying correct values([e0d125e](https://github.com/ofkm/arcane/commit/e0d125e5793f242efc787304664fe7166843dfa4) by @kmendell)
- update size classes for consistency across components([3ceb93d](https://github.com/ofkm/arcane/commit/3ceb93df9bbf3bfce040ca2d277670786bf116db) by @kmendell)
- make font size more consistant in editors([efbbe75](https://github.com/ofkm/arcane/commit/efbbe75217f292e8a38fb2b6b8985b7e6c79e264) by @kmendell)
- container log performance issues ([#222](https://github.com/ofkm/arcane/pull/222) by @kmendell)
- remove old references to app-settings.json([65f20d1](https://github.com/ofkm/arcane/commit/65f20d1415b4f174172b212a425949356131d03a) by @kmendell)

### Other

- remove tailwind config from components.json([f58ddca](https://github.com/ofkm/arcane/commit/f58ddca47bfe94b9acaf86496024285e6f218960) by @kmendell)
- bump the prod-dependencies group with 2 updates ([#220](https://github.com/ofkm/arcane/pull/220) by @dependabot[bot])
- fix types of converter service([9b10e2a](https://github.com/ofkm/arcane/commit/9b10e2a1d13ddc020ad6c1d7f937dac431a510c1) by @kmendell)
- fix lexical analysis in converter service([d02e8d0](https://github.com/ofkm/arcane/commit/d02e8d0b83b15dc2ff63a3beb5236be266d0e24d) by @kmendell)
- update imports from 'bits-ui' to '$lib/utils.js' for consistency([448500c](https://github.com/ofkm/arcane/commit/448500ceb9c71f60547868bb96aafd4081c39bfe) by @kmendell)
- run formatter([9283765](https://github.com/ofkm/arcane/commit/9283765cf9a05518376a0346e27bfe2b9bc9656e) by @kmendell)
- revert lucide svelte version([40e3f30](https://github.com/ofkm/arcane/commit/40e3f30f4c8d0ee8908a3fde5f47c88e38c57d1c) by @kmendell)
- revert lucide svelte version([e2a5d53](https://github.com/ofkm/arcane/commit/e2a5d53520d99e1dffe648138123f4caca385427) by @kmendell)
- bump the prod-dependencies group with 2 updates ([#223](https://github.com/ofkm/arcane/pull/223) by @dependabot[bot])
- bump the prod-dependencies group with 2 updates ([#224](https://github.com/ofkm/arcane/pull/224) by @dependabot[bot])
- bump bits-ui from 1.8.0 to 2.1.0 in the frontend-major-updates group ([#225](https://github.com/ofkm/arcane/pull/225) by @dependabot[bot])
- update tailwindcss to version 4.1.7 and refactor class names for consistency([f585b94](https://github.com/ofkm/arcane/commit/f585b94cd875c23e1df91cf6fc470fd2a9581455) by @kmendell)
- bump the prod-dependencies group with 4 updates ([#228](https://github.com/ofkm/arcane/pull/228) by @dependabot[bot])
- add new screenshot([d9e826a](https://github.com/ofkm/arcane/commit/d9e826ab5d69b1e8fd74c5a0d7b8a70f6e1b429d) by @kmendell)

**Full Changelog**: https://github.com/ofkm/arcane/compare/v0.12.0...v0.13.0

## v0.12.0

### New features

- stack logs tab([696d74a](https://github.com/ofkm/arcane/commit/696d74a2391acae08c1fb306a6ae26463bf5bf50) by @kmendell)
- convert docker run to docker compose ([#219](https://github.com/ofkm/arcane/pull/219) by @kmendell)

### Bug fixes

- external networks names not being respected([712fa00](https://github.com/ofkm/arcane/commit/712fa001b672212a98d61633dca96d3092b29a22) by @kmendell)

**Full Changelog**: https://github.com/ofkm/arcane/compare/v0.11.1...v0.12.0

## v0.11.1

### Bug fixes

- largest images differ on dashboard and container images([9ffd0f6](https://github.com/ofkm/arcane/commit/9ffd0f68378518e2b717a078d4800578395228ba) by @kmendell)

**Full Changelog**: https://github.com/ofkm/arcane/compare/v0.11.0...v0.11.1

## v0.11.0

### New features

- save page sizes for all tables([e01d7eb](https://github.com/ofkm/arcane/commit/e01d7ebd05d93f1b9a716ec8dae4535c8e0e1f2a) by @kmendell)

### Bug fixes

- stack deployments for external networks ([#199](https://github.com/ofkm/arcane/pull/199) by @kmendell)
- use correct stack api endpoints([4ea2c12](https://github.com/ofkm/arcane/commit/4ea2c125319df1b797842f649f43ae649414bbd4) by @kmendell)
- cleanup failed stack deployments if they fail([6fa7bd7](https://github.com/ofkm/arcane/commit/6fa7bd71d511e1b0286e07e6165470c6abfdada0) by @kmendell)
- dashboard overview card arrangement ([#215](https://github.com/ofkm/arcane/pull/215) by @kmendell)

### Dependencies

- bump ajinabraham/njsscan-action from 6 to 9 ([#202](https://github.com/ofkm/arcane/pull/202) by @dependabot[bot])

### Other

- bump the dev-dependencies group with 2 updates ([#201](https://github.com/ofkm/arcane/pull/201) by @dependabot[bot])
- upgrade deps excluding svelte([aee3b6d](https://github.com/ofkm/arcane/commit/aee3b6d3ad2ac92fb1d65ee2db5ee0abddbae5b7) by @kmendell)
- fix the existing e2e tests([338dc57](https://github.com/ofkm/arcane/commit/338dc57b6bd30ab5298b9c7bdd647a5b34ac2c71) by @kmendell)
- update workflow to use built int docker([284f2c6](https://github.com/ofkm/arcane/commit/284f2c6364826c5bb7466e8d2c93d278649e318f) by @kmendell)
- create .env in test workflow([2f2e2aa](https://github.com/ofkm/arcane/commit/2f2e2aa5ddde0f4f4f9d9712cbf0564f835e48c1) by @kmendell)
- cleanup comments and logging([540650b](https://github.com/ofkm/arcane/commit/540650b4261f48d5f2703f0d80d76de03c9b537d) by @kmendell)
- custom stack implementation ([#208](https://github.com/ofkm/arcane/pull/208) by @kmendell)
- bump the prod-dependencies group across 1 directory with 3 updates ([#216](https://github.com/ofkm/arcane/pull/216) by @dependabot[bot])
- bump the prod-dependencies group with 3 updates ([#217](https://github.com/ofkm/arcane/pull/217) by @dependabot[bot])
- remove tailwind.config.ts([95bcc32](https://github.com/ofkm/arcane/commit/95bcc32efe93d4d9636b4a555527b2abf38921bd) by @kmendell)

**Full Changelog**: https://github.com/ofkm/arcane/compare/v0.10.0...v0.11.0

## v0.10.0

### New features

- environment variable support in compose files ([#195](https://github.com/ofkm/arcane/pull/195) by @kmendell)

### Bug fixes

- table selection states not getting invailidated([d1ef3cb](https://github.com/ofkm/arcane/commit/d1ef3cb4a0656bfb95736dcd0fcaab11649d4e18) by @kmendell)
- stacks not starting with more than one network ([#191](https://github.com/ofkm/arcane/pull/191) by @kmendell)
- improve loading speed of stack and container pages ([#194](https://github.com/ofkm/arcane/pull/194) by @kmendell)

### Documentation

- clean up and redesign docs ([#189](https://github.com/ofkm/arcane/pull/189) by @kmendell)

### Other

- change header text to be more generalized([78835b3](https://github.com/ofkm/arcane/commit/78835b34d99e5d754c594353ab06fe20c76290c7) by @kmendell)
- bump the prod-dependencies group with 3 updates ([#192](https://github.com/ofkm/arcane/pull/192) by @dependabot[bot])
- add nodejs scan([0268696](https://github.com/ofkm/arcane/commit/026869685a61580ca12946782c59716d116f3cde) by @kmendell)
- eslint fixes([f9c8361](https://github.com/ofkm/arcane/commit/f9c8361d0d7fe07d2d849dc74cbb913e2745c577) by @kmendell)
- more eslint fixes([c31defe](https://github.com/ofkm/arcane/commit/c31defe0dcde2c6a6093475ea25f1bab5076855e) by @kmendell)
- clean up and eslint fixes([3426e87](https://github.com/ofkm/arcane/commit/3426e870a34a001f125648996227d0517c8fd4bf) by @kmendell)
- more eslint fixes([8bc6621](https://github.com/ofkm/arcane/commit/8bc6621fe114f38014daaf9c3eaa5b902bafb2d6) by @kmendell)
- remove required props([4779008](https://github.com/ofkm/arcane/commit/4779008e9e621bdf15b6fc0968cff8139a7985f4) by @kmendell)
- bump eslint-plugin-svelte from 3.6.0 to 3.7.0 in the dev-dependencies group ([#193](https://github.com/ofkm/arcane/pull/193) by @dependabot[bot])
- update button styles ([#196](https://github.com/ofkm/arcane/pull/196) by @kmendell)
- remove unused imports([f61cc22](https://github.com/ofkm/arcane/commit/f61cc224a527af51eb763cb01bba3f1b1a21bbb4) by @kmendell)

**Full Changelog**: https://github.com/ofkm/arcane/compare/v0.9.2...v0.10.0

## v0.9.2

### Bug fixes

- cache image maturity results to improve page loading([ee6eb1e](https://github.com/ofkm/arcane/commit/ee6eb1e52d31aa319f8148bce902e2d5696b97d4) by @kmendell)

### Documentation

- fix sidebar on mobile devices([eafc076](https://github.com/ofkm/arcane/commit/eafc076e87dedb2260b0f9a92e8eca4278a98bea) by @kmendell)

### Dependencies

- bump prettier-plugin-svelte from 3.3.3 to 3.4.0 in the dev-dependencies group ([#188](https://github.com/ofkm/arcane/pull/188) by @dependabot[bot])

### Other

- bump the prod-dependencies group with 2 updates ([#187](https://github.com/ofkm/arcane/pull/187) by @dependabot[bot])

**Full Changelog**: https://github.com/ofkm/arcane/compare/v0.9.1...v0.9.2

## v0.9.1

### Bug fixes

- settings not loading values from json file([ad65e80](https://github.com/ofkm/arcane/commit/ad65e803c53e5c9f8b639e0620b6e85a61b50941) by @kmendell)
- volumes cant be deleted from volume details page([76bc5b8](https://github.com/ofkm/arcane/commit/76bc5b8e362b8e8de216d18d7f6acf9fd86d171b) by @kmendell)

**Full Changelog**: https://github.com/ofkm/arcane/compare/v0.9.0...v0.9.1

## v0.9.0

### New features

- add logged in users name in sidebar([e86659b](https://github.com/ofkm/arcane/commit/e86659bd94b3d4918ab17c3243d0e3c7a7512cf0) by @kmendell)
- oidc login support ([#172](https://github.com/ofkm/arcane/pull/172) by @kmendell)
- add dark / light mode toggle([f24cae1](https://github.com/ofkm/arcane/commit/f24cae1afcb26090005fe9fab4a9376a6725f749) by @kmendell)
- image maturity indicator and image update indicators ([#181](https://github.com/ofkm/arcane/pull/181) by @kmendell)

### Bug fixes

- selectedIds persisting after api call is returned([0c01485](https://github.com/ofkm/arcane/commit/0c0148504e29a165afef75b9d07c2794e4371335) by @kmendell)
- stacks not deploying if a health check is defined([664f330](https://github.com/ofkm/arcane/commit/664f330ac8fef08c71bc8f35b401978f4c9e44bd) by @kmendell)
- use svg icon over png([da5a591](https://github.com/ofkm/arcane/commit/da5a591327b2ac78c7ca89a017e6bc7d24d40d6d) by @kmendell)
- truncate long images names on dashboard table([87556c9](https://github.com/ofkm/arcane/commit/87556c94e9f3449977a8f035c0597834c1d82675) by @kmendell)
- sort images and containers but uptime and size([05cc599](https://github.com/ofkm/arcane/commit/05cc5992134040d76624b2e4525b071e0da1cc00) by @kmendell)
- duplicated service badge links on stack start([aca8932](https://github.com/ofkm/arcane/commit/aca8932a787703824c3f35c44851cc37407fabc7) by @kmendell)
- container logs duplicated and not formatted correctly([5aa5f04](https://github.com/ofkm/arcane/commit/5aa5f048f6c4b4ca36183c682daf1d3418cc1737) by @kmendell)
- UI consistency and layout updates ([#185](https://github.com/ofkm/arcane/pull/185) by @kmendell)

### Documentation

- add oidc documentation([f8f60bd](https://github.com/ofkm/arcane/commit/f8f60bd11726342c5e252af6970aebf247945cff) by @kmendell)
- add oidc documentation for ui config([5e3c6ba](https://github.com/ofkm/arcane/commit/5e3c6ba8005c78bcb663340992620f0ca4fea700) by @kmendell)

### Dependencies

- bump docker/build-push-action from 5 to 6 ([#182](https://github.com/ofkm/arcane/pull/182) by @dependabot[bot])

### Other

- cleanup some old comments([50cb424](https://github.com/ofkm/arcane/commit/50cb42468808107d72411250241811e97a3f1287) by @kmendell)
- update svelte-sonner to 1.0.0([3ad26ce](https://github.com/ofkm/arcane/commit/3ad26ce7b5a0b08c2a631c6322ae32215e77b1df) by @kmendell)
- bump @lucide/svelte from 0.509.0 to 0.510.0 in the prod-dependencies group across 1 directory ([#178](https://github.com/ofkm/arcane/pull/178) by @dependabot[bot])
- update tab list ui([d131d85](https://github.com/ofkm/arcane/commit/d131d85e634488d4e51647820a9d34ff63f82cb3) by @kmendell)
- add build workflow for next image([35cef3a](https://github.com/ofkm/arcane/commit/35cef3ad47dab582cf9ae509051c7ce959bd59d1) by @kmendell)
- set dummy env variables for docker build([6f299ed](https://github.com/ofkm/arcane/commit/6f299ed7386348c1f788c851017fa5f6f40d0e22) by @kmendell)
- disable blank issues([cb6023d](https://github.com/ofkm/arcane/commit/cb6023d388608de69d90b602ee67bbfa9d1c3321) by @kmendell)
- upgrade shadcn-svelte and tailwind classes ([#183](https://github.com/ofkm/arcane/pull/183) by @kmendell)

**Full Changelog**: https://github.com/ofkm/arcane/compare/v0.8.0...v0.9.0

## v0.8.0

### New features

- private docker registries ([#162](https://github.com/ofkm/arcane/pull/162) by @kmendell)
- remove delete button from actions dropdown menu for bulk removing ([#169](https://github.com/ofkm/arcane/pull/169) by @kmendell)
- used/unused filtering for images and volumes ([#170](https://github.com/ofkm/arcane/pull/170) by @kmendell)

### Bug fixes

- use uid/gid 200 in container ([#156](https://github.com/ofkm/arcane/pull/156) by @kmendell)
- importing stacks if files are in the data/stacks directory ([#161](https://github.com/ofkm/arcane/pull/161) by @kmendell)
- remove id columns from dashboard tables([a414cbb](https://github.com/ofkm/arcane/commit/a414cbb5777468b0e2cd4346eac83ba709f03eaa) by @kmendell)
- dockerhost from settings not being respected ([#171](https://github.com/ofkm/arcane/pull/171) by @kmendell)

### Documentation

- update quickstart with latest configuration([34dd97c](https://github.com/ofkm/arcane/commit/34dd97c2d60a9781bd9317157505b13136040f46) by @kmendell)
- fix volume mapping typos([435fb34](https://github.com/ofkm/arcane/commit/435fb349f990f4b7ed15223c33c3bea0a58739ec) by @kmendell)

### Other

- remove log-level from compose file([694a764](https://github.com/ofkm/arcane/commit/694a764bb95e66a228b4a4f8b28b10c397a24b17) by @kmendell)
- add arcane.svg ([6f7af5d](https://github.com/ofkm/arcane/commit/6f7af5d427634028c66d50fdff60c370b7fe9f5c) by @kmendell)
- bump the prod-dependencies group across 1 directory with 7 updates ([#167](https://github.com/ofkm/arcane/pull/167) by @dependabot[bot])

**Full Changelog**: https://github.com/ofkm/arcane/compare/v0.7.1...v0.8.0

## v0.7.1

### Bug fixes

- show error messages in toasts ([#148](https://github.com/ofkm/arcane/pull/148) by @kmendell)
- compose stacks not starting from the stack directory([7090c4e](https://github.com/ofkm/arcane/commit/7090c4e0950274e0334bc229e5c3b1435ee3e22d) by @kmendell)
- container permissions and removed the need for DOCKER_GID([dde20c0](https://github.com/ofkm/arcane/commit/dde20c0cff5dea6812e29677dae8254ad41abaa1) by @kmendell)

### Other

- remove pull requests from eslint([76e27c9](https://github.com/ofkm/arcane/commit/76e27c9859acea0a6f227fd6c20a14ee25b34235) by @kmendell)
- add dependabot groups for npm([3808fc1](https://github.com/ofkm/arcane/commit/3808fc11687221bbd2f8842f9b70c1aadce06f67) by @kmendell)

**Full Changelog**: https://github.com/ofkm/arcane/compare/v0.7.0...v0.7.1

## v0.7.0

### New features

- container service link for stacks ([#131](https://github.com/ofkm/arcane/pull/131) by @kmendell)
- use stack names as folder names ([#143](https://github.com/ofkm/arcane/pull/143) by @kmendell)

### Bug fixes

- use data in relative path for base directory([7f8dd2c](https://github.com/ofkm/arcane/commit/7f8dd2cb213476ee30baac7faee990d41089d703) by @kmendell)
- use data in relative path for base directory([29ba132](https://github.com/ofkm/arcane/commit/29ba132eae64fc6eb3e1da57455623b9d3eeeab4) by @kmendell)
- container logs not streaming from server ([#138](https://github.com/ofkm/arcane/pull/138) by @kmendell)
- container stats not live updating ([#139](https://github.com/ofkm/arcane/pull/139) by @kmendell)
- onboarding errors and protections ([#142](https://github.com/ofkm/arcane/pull/142) by @kmendell)
- redirect to list view after removing a container or stack([0fa0f03](https://github.com/ofkm/arcane/commit/0fa0f03aa36c6a4da482d06226c075921c232c1e) by @kmendell)
- remove stack name link when its external([7499aee](https://github.com/ofkm/arcane/commit/7499aeeac6ebda4fa1fd5b24cb71b44a1aca30a2) by @kmendell)

### Other

- remove data files([9422299](https://github.com/ofkm/arcane/commit/9422299722fb2b7742a9d853053e040f98a7f704) by @kmendell)

**Full Changelog**: https://github.com/ofkm/arcane/compare/v0.6.0...v0.7.0

## v0.6.0

### New features

- .env configuration support ([#128](https://github.com/ofkm/arcane/pull/128) by @kmendell)

### Bug fixes

- remove ping group and iputils from container so GID 999 is available([4a9e619](https://github.com/ofkm/arcane/commit/4a9e6194cf38a00b4e3a8e71cabd72dd9c896e52) by @kmendell)
- user creation button not showing loading state([d79b2ff](https://github.com/ofkm/arcane/commit/d79b2ff76825f88fbe0c833515a17458bdef5002) by @kmendell)
- password policy not able to be saved([976cd83](https://github.com/ofkm/arcane/commit/976cd831bc064062a0329d0975dd1b64dd17bd32) by @kmendell)

### Other

- bump vite from 6.3.4 to 6.3.5 ([#123](https://github.com/ofkm/arcane/pull/123) by @dependabot[bot])
- bump typescript-eslint from 8.31.1 to 8.32.0 ([#120](https://github.com/ofkm/arcane/pull/120) by @dependabot[bot])
- bump eslint from 9.25.1 to 9.26.0 ([#124](https://github.com/ofkm/arcane/pull/124) by @dependabot[bot])
- bump mode-watcher from 1.0.4 to 1.0.6 ([#121](https://github.com/ofkm/arcane/pull/121) by @dependabot[bot])

**Full Changelog**: https://github.com/ofkm/arcane/compare/v0.5.0...v0.6.0

## v0.5.0

### New features

- add confiramtion dialog before stopping all running containers([1a696c0](https://github.com/ofkm/arcane/commit/1a696c08e7b15f13bfdf4b0542d444facbeeb851) by @kmendell)

### Bug fixes

- use correct cursor on all buttons([50d4211](https://github.com/ofkm/arcane/commit/50d4211c23743c1e5fda6324be9220e7e367ae05) by @kmendell)
- loading states on action buttons not reflecting status([8305078](https://github.com/ofkm/arcane/commit/8305078dcd1fd07a89976466d90350d5e05e0b3f) by @kmendell)
- session cookie not being created on http sites ([#112](https://github.com/ofkm/arcane/pull/112) by @kmendell)

### Documentation

- add analytics to doc site([a7b381b](https://github.com/ofkm/arcane/commit/a7b381b9102c947e32914d46f690d9a06384164f) by @kmendell)

### Other

- update release script to use correct syntax([3a7c1b3](https://github.com/ofkm/arcane/commit/3a7c1b3b8b5898ade3091919870e5fd240dfa8b5) by @kmendell)

**Full Changelog**: https://github.com/ofkm/arcane/compare/v0.4.1...v0.5.0

## v0.4.1

### Bug fixes

- adjust ownership handling in entrypoint script([a3ec54a](https://github.com/ofkm/arcane/commit/a3ec54a058548a66ae9e637cdd6e34228c5e995b) by @kmendell)

### Documentation

- update wording of image features([c8ddbc9](https://github.com/ofkm/arcane/commit/c8ddbc92feda40d4e44100b2160998b1896dc6de) by @kmendell)
- update docker compose layout([8e0eaa7](https://github.com/ofkm/arcane/commit/8e0eaa7048788226b6c3f4fa126f83775c1eca7f) by @kmendell)

### Other

- add screenshot to readme([b3ade80](https://github.com/ofkm/arcane/commit/b3ade80285d4720f9590eea7a34575b2c9b62621) by @kmendell)
- update screenshot([3cf0c13](https://github.com/ofkm/arcane/commit/3cf0c13f8cb90f54bb87d7fbb1736994e1b94ade) by @kmendell)

**Full Changelog**: https://github.com/ofkm/arcane/compare/v0.4.0...v0.4.1

## v0.4.0

### New features

- image inspection page([c3f5665](https://github.com/ofkm/arcane/commit/c3f5665bf5c67077a9d21d33dc23ec0a530ea041) by @kmendell)
- volume inspection page ([#75](https://github.com/ofkm/arcane/pull/75) by @kmendell)
- network inspection page ([#76](https://github.com/ofkm/arcane/pull/76) by @kmendell)
- dashboard quick actions ([#77](https://github.com/ofkm/arcane/pull/77) by @kmendell)
- auto update containers and stacks ([#83](https://github.com/ofkm/arcane/pull/83) by @kmendell)
- user authentication ([#86](https://github.com/ofkm/arcane/pull/86) by @kmendell)

### Bug fixes

- show ipvaln/macvlan ip address on details page([77495da](https://github.com/ofkm/arcane/commit/77495da4b40a7da02a50deea336ced9a7885abe5) by @kmendell)
- disable autofill for input fields([c7ff1c0](https://github.com/ofkm/arcane/commit/c7ff1c063161a1bca8aff4b426db0011e8b19f48) by @kmendell)
- set default restart policy to unless-stopped([4c578e4](https://github.com/ofkm/arcane/commit/4c578e4d9faca22333f15f011ed2c98c46c3ebb3) by @kmendell)
- use for watching containers([e99ec10](https://github.com/ofkm/arcane/commit/e99ec10787af07e4f4e27e7fb8195c45ccde00c7) by @kmendell)
- make sure data is watched by effect([cd89d48](https://github.com/ofkm/arcane/commit/cd89d48677c1ebb899979663e7ddaeba9410705d) by @kmendell)
- container table showing when no containers found([4f63742](https://github.com/ofkm/arcane/commit/4f63742dd584a5d598876497b46c6d6090503938) by @kmendell)

### Documentation

- cleanup and fix some documentation([37e8ccd](https://github.com/ofkm/arcane/commit/37e8ccdd25b1a20774b5237b270835918a56f722) by @kmendell)
- fix sidebar([ffcd012](https://github.com/ofkm/arcane/commit/ffcd012dd619d7efbbe69dcce73c2497e43959d7) by @kmendell)
- remove architecture file([353b11a](https://github.com/ofkm/arcane/commit/353b11a38d4e630ecf80d6957c23e212eb458074) by @kmendell)
- update install guide and building docs([0249b58](https://github.com/ofkm/arcane/commit/0249b5839a8be55c37451abee3a905319efe1189) by @kmendell)
- fix typos and incorrect items([6079a52](https://github.com/ofkm/arcane/commit/6079a5290344c9d2a7f26b7a2908eba47acc02b1) by @kmendell)
- update guides section with troubleshootig info([5facc19](https://github.com/ofkm/arcane/commit/5facc198906014099b9f9b1fb10b0c5c02f93f69) by @kmendell)
- update note about registry credentials([6e0e5aa](https://github.com/ofkm/arcane/commit/6e0e5aaf207a1e9bd13cfccd19b68d407f125fd0) by @kmendell)

### Dependencies

- bump actions/cache from 3 to 4 ([#81](https://github.com/ofkm/arcane/pull/81) by @dependabot[bot])

### Other

- update dockerfile with labels([cc23955](https://github.com/ofkm/arcane/commit/cc239556f404f575f2bf3b9de6873e4c9803a5ef) by @kmendell)
- update readme([b785b79](https://github.com/ofkm/arcane/commit/b785b794d2a2cb6af46b4a3263b0002fb434b79e) by @kmendell)
- add assets for readme([05b95e4](https://github.com/ofkm/arcane/commit/05b95e4e0ae0d1d968d2bfa29009c0a703afc395) by @kmendell)
- create SECURITY.md([ac700eb](https://github.com/ofkm/arcane/commit/ac700ebffdf04d8991e1fe257262853befc72626) by @kmendell)
- add license([ae19fa1](https://github.com/ofkm/arcane/commit/ae19fa164fe4b6ff4009836e8575c94028f41eb2) by @kmendell)
- add docker and gha to dependabot([9956ffa](https://github.com/ofkm/arcane/commit/9956ffa911c841514e0a8b64dbc437f065cec947) by @kmendell)
- bump bits-ui from 1.3.19 to 1.4.0 ([#84](https://github.com/ofkm/arcane/pull/84) by @dependabot[bot])
- bump typescript-eslint from 8.31.0 to 8.31.1 ([#85](https://github.com/ofkm/arcane/pull/85) by @dependabot[bot])
- reate codeql.yml([75def23](https://github.com/ofkm/arcane/commit/75def235f539f5ad1d4884c1b7597064cd19b6d2) by @kmendell)
- bump bits-ui from 1.4.0 to 1.4.2 ([#87](https://github.com/ofkm/arcane/pull/87) by @dependabot[bot])
- bump sveltekit-superforms from 2.24.1 to 2.25.0 ([#88](https://github.com/ofkm/arcane/pull/88) by @dependabot[bot])
- add FUNDING.yml([d881fba](https://github.com/ofkm/arcane/commit/d881fbabda6b6d004122e1bd26265ebe481798ce) by @kmendell)
- add eslint workflow([fffebad](https://github.com/ofkm/arcane/commit/fffebadbef055f7ade70e172bcdd26047a9d5c28) by @kmendell)
- change eslint to run on ubuntu 22.04([3880e67](https://github.com/ofkm/arcane/commit/3880e675b87c7cfb8f8dd012896b291e4ccb36eb) by @kmendell)
- update package-lock.json([2f57c13](https://github.com/ofkm/arcane/commit/2f57c134851409a851d0ce94ad417d8759237c41) by @kmendell)
- bump bits-ui from 1.4.2 to 1.4.3 ([#91](https://github.com/ofkm/arcane/pull/91) by @dependabot[bot])
- bump @sveltejs/kit from 2.20.7 to 2.20.8 ([#92](https://github.com/ofkm/arcane/pull/92) by @dependabot[bot])
- add issue type to feature template([27a5082](https://github.com/ofkm/arcane/commit/27a50824255fafcfb32d4b0b1f9fec675c722c9b) by @kmendell)
- add issue type to bug template([5ddbb9d](https://github.com/ofkm/arcane/commit/5ddbb9dd7d867210dd132c5b022b6c8b0cb4f78f) by @kmendell)
- update readme layout([fd6b77a](https://github.com/ofkm/arcane/commit/fd6b77acff75beaae13c1f37b8453ef411f89ee4) by @kmendell)
- update eslint workflow([3068a8e](https://github.com/ofkm/arcane/commit/3068a8e36c1e455b4fe385ef65d66646b747020e) by @kmendell)
- Rename contributing.md to CONTRIBUTING.md([aae2e5a](https://github.com/ofkm/arcane/commit/aae2e5a4ebf99c0f30291f636ebf80ecca5a9008) by @kmendell)
- remove header from contributing([7ce2d2a](https://github.com/ofkm/arcane/commit/7ce2d2a18c58a133b90f0ee1970a83f8bfce0230) by @kmendell)
- add svelte-check matcher([dbf6a68](https://github.com/ofkm/arcane/commit/dbf6a68bb18e27e2c3880e14e164a6f34355a8df) by @kmendell)
- add svelte-check workflow([d41a915](https://github.com/ofkm/arcane/commit/d41a915786c076c7aa5bc7eadd10a4d662a37dc5) by @kmendell)
- update eslint workflow([8953180](https://github.com/ofkm/arcane/commit/895318067e33b0571e0b1d3b2fca329f640f3fee) by @kmendell)
- bump eslint version([307663d](https://github.com/ofkm/arcane/commit/307663dc409156fe02af14407c41d442e2e6019b) by @kmendell)
- api, tables, and ui cleanup and refactor code, add e2e tests ([#96](https://github.com/ofkm/arcane/pull/96) by @kmendell)
- add note about private registries([469a5e9](https://github.com/ofkm/arcane/commit/469a5e920119ebd7f646af97205e3675720b52b3) by @kmendell)
- bump @lucide/svelte from 0.503.0 to 0.507.0 ([#103](https://github.com/ofkm/arcane/pull/103) by @dependabot[bot])
- bump svelte-check from 4.1.6 to 4.1.7 ([#99](https://github.com/ofkm/arcane/pull/99) by @dependabot[bot])
- bump bits-ui from 1.4.3 to 1.4.6 ([#101](https://github.com/ofkm/arcane/pull/101) by @dependabot[bot])
- bump zod from 3.24.3 to 3.24.4 ([#102](https://github.com/ofkm/arcane/pull/102) by @dependabot[bot])

**Full Changelog**: https://github.com/ofkm/arcane/compare/v0.3.0...v0.4.0

## v0.3.0

### New features

- settings page overhaul ([#48](https://github.com/ofkm/arcane/pull/48) by @kmendell)
- add in-use/unused badge on volumes and images([75ea68b](https://github.com/ofkm/arcane/commit/75ea68b04164f734af5d9fdc560fd983620d6a96) by @kmendell)
- add create container logic ([#53](https://github.com/ofkm/arcane/pull/53) by @kmendell)
- mass-delete images, volumes, and networks ([#69](https://github.com/ofkm/arcane/pull/69) by @kmendell)
- add container resource card([37ec736](https://github.com/ofkm/arcane/commit/37ec736c4bb0c58bb2bf65681e04ad5e4bd280a1) by @kmendell)

### Bug fixes

- unused badge color and layout([67e5bc5](https://github.com/ofkm/arcane/commit/67e5bc552f814af8d4e77a26f415c58a65c4de4f) by @kmendell)
- add link to container details in table column([72bd842](https://github.com/ofkm/arcane/commit/72bd8425439fdbca4cf44af2035869d0e5dc9406) by @kmendell)
- custom badge colors and look([a1e59bd](https://github.com/ofkm/arcane/commit/a1e59bd895fa681332b13dd8a6e668ef885a5c14) by @kmendell)
- remove badges on detail pages([4404bd2](https://github.com/ofkm/arcane/commit/4404bd268b61b79118198b52899585f05f21b2ab) by @kmendell)
- remove badges on detail pages([1da4c79](https://github.com/ofkm/arcane/commit/1da4c791f22a03214953a3786bfa1bb9dc54f062) by @kmendell)
- remove docker connected label from sidebar([13d9060](https://github.com/ofkm/arcane/commit/13d90601af75c0df2ae7eefc389778b567ff5ddb) by @kmendell)
- use new status badge instead of custom-badge([90303a6](https://github.com/ofkm/arcane/commit/90303a64a749bcc9754ce8cdaa3a1597c5dbbe9b) by @kmendell)
- disable checkbox if stack is external([93884eb](https://github.com/ofkm/arcane/commit/93884ebeaffec8412f1e4ab6371b6d2c19cf596d) by @kmendell)
- show total image size in dashboard card([9e4749c](https://github.com/ofkm/arcane/commit/9e4749ccd42538e3b44b2e919322daff8da40220) by @kmendell)
- show docker engine version in card([8e5fc5b](https://github.com/ofkm/arcane/commit/8e5fc5b3eea8db26f11ebfe951402093addd97f6) by @kmendell)
- image pulling not repecting user defined tag([41af290](https://github.com/ofkm/arcane/commit/41af2909f79b8e6e5d4fc7bc0a0e26193ad59dd0) by @kmendell)
- stacks not saving on edit([ba13d7b](https://github.com/ofkm/arcane/commit/ba13d7bbda14349d5f25cafa74ce74284448bc38) by @kmendell)
- restore yaml editor functionality([f0484ec](https://github.com/ofkm/arcane/commit/f0484ecca5ca399769d73ca7ac6c164c4b5b3bc9) by @kmendell)
- add api endpoints for stacks and container actions ([#73](https://github.com/ofkm/arcane/pull/73) by @kmendell)

### Documentation

- setup documentation WIP ([#72](https://github.com/ofkm/arcane/pull/72) by @kmendell)

### Other

- bump svelte from 5.28.1 to 5.28.2 ([#47](https://github.com/ofkm/arcane/pull/47) by @dependabot[bot])
- bump vite from 6.3.2 to 6.3.3 ([#55](https://github.com/ofkm/arcane/pull/55) by @dependabot[bot])
- use neutral theme([de3952a](https://github.com/ofkm/arcane/commit/de3952a9c890dc1d4fb0e40c7139d29419c010fb) by @kmendell)
- cleanup dashboard links([5ec6edc](https://github.com/ofkm/arcane/commit/5ec6edc79bdd33202f533926807bf9017c17cbb5) by @kmendell)
- change wording of readme([b42c013](https://github.com/ofkm/arcane/commit/b42c01393df8dfbd3d21b7ea8686f0351e9c8744) by @kmendell)
- fix docker-service([5acfd98](https://github.com/ofkm/arcane/commit/5acfd98e8e2aecfbabc6b26256d33a7809545ad8) by @kmendell)
- remove unused CSS variables and styles([25170a3](https://github.com/ofkm/arcane/commit/25170a3c1c6cd99af184c57efdd6d1105cf8bac6) by @kmendell)
- add prettier formatting([215221d](https://github.com/ofkm/arcane/commit/215221da866380eb5e60e0e3f8eaba75a6c4addd) by @kmendell)
- clean up unused imports and comments across multiple components([9a1afa6](https://github.com/ofkm/arcane/commit/9a1afa68e3f31c733029f22547b6d3da2e3ae3c8) by @kmendell)
- clean up import statements in docker-service.ts([99d3f76](https://github.com/ofkm/arcane/commit/99d3f76dd5c3e6014c9b01e46683d79300cb7cb1) by @kmendell)
- reorganize services and types ([#71](https://github.com/ofkm/arcane/pull/71) by @kmendell)
- remove npmrc([a452c2d](https://github.com/ofkm/arcane/commit/a452c2dc2c9ccb1a2603bf058cd35b9efdaab775) by @kmendell)
- format code([6308a6a](https://github.com/ofkm/arcane/commit/6308a6ae3cb134e185d923e5a43b446f628ead58) by @kmendell)
- remove unused imports([8ce5629](https://github.com/ofkm/arcane/commit/8ce562978485af1825acaf849e0847f9f6a5b152) by @kmendell)

**Full Changelog**: https://github.com/ofkm/arcane/compare/v0.2.0...v0.3.0

## v0.2.0

### New features

- link stack containers to container details page ([#41](https://github.com/ofkm/arcane/pull/41) by @kmendell)
- add yaml editor component ([#44](https://github.com/ofkm/arcane/pull/44) by @kmendell)
- implement full docker compose spec ([#45](https://github.com/ofkm/arcane/pull/45) by @kmendell)

### Other

- bump typescript-eslint from 8.30.1 to 8.31.0 ([#38](https://github.com/ofkm/arcane/pull/38) by @dependabot[bot])
- bump eslint from 9.25.0 to 9.25.1 ([#37](https://github.com/ofkm/arcane/pull/37) by @dependabot[bot])
- improve dockerfile([1a66037](https://github.com/ofkm/arcane/commit/1a66037f6e01a25efa4370190ebf6faea1a742be) by @kmendell)

**Full Changelog**: https://github.com/ofkm/arcane/compare/v0.1.1...v0.2.0

## v0.1.1

### Bug fixes

- container details card layout([94fde0e](https://github.com/ofkm/arcane/commit/94fde0e470043b519d27dfd6e5aa0c27779496ae) by @kmendell)
- container env details layout([b2f7e28](https://github.com/ofkm/arcane/commit/b2f7e28fd31875c768c845ab4cbbc8e99406ea01) by @kmendell)
- container port details layout([721cacb](https://github.com/ofkm/arcane/commit/721cacb35292ec06060afbc6fa0b4f7fbddaca08) by @kmendell)
- make container details grid more reponsive([17b5abf](https://github.com/ofkm/arcane/commit/17b5abf55d5f37a6f02ae4d7f236c9a65beb63ba) by @kmendell)
- log display improvements([8343c9d](https://github.com/ofkm/arcane/commit/8343c9d3dd71cb85297de817a97c764b455848b6) by @kmendell)

### Other

- bump @lucide/svelte from 0.501.0 to 0.503.0 ([#39](https://github.com/ofkm/arcane/pull/39) by @dependabot[bot])
- add release workflow([0fa9aae](https://github.com/ofkm/arcane/commit/0fa9aaea598fc61a2105b2cc3f3f82465e3255a9) by @kmendell)

**Full Changelog**: https://github.com/ofkm/arcane/compare/v0.1.0...v0.1.1

## v0.1.0

### New features

- add container logs([213dc2f](https://github.com/ofkm/arcane/commit/213dc2f8bdef9540a2a64f40d9b57d949ff889db) by @kmendell)
- application settings page([10084f8](https://github.com/ofkm/arcane/commit/10084f832b11c02eb93a3aa1b76f644497ce0f6e) by @kmendell)
- redesign dashboard page([5ed6aa1](https://github.com/ofkm/arcane/commit/5ed6aa12aacac5aea6dee682336b115e1f8c4ce4) by @kmendell)
- redesign ui to be more modern([942d674](https://github.com/ofkm/arcane/commit/942d674792f9be891f76d5ac581969a7e99e7608) by @kmendell)
- update sidebar ui([9547de6](https://github.com/ofkm/arcane/commit/9547de6ae30e60f8560277a0dd365be077989730) by @kmendell)
- initial stakc implementation([924b3f1](https://github.com/ofkm/arcane/commit/924b3f1483d79a4ae314b2be16d4f2da1ea4acca) by @kmendell)
- configurable stack directory ([#13](https://github.com/ofkm/arcane/pull/13) by @kmendell)
- create and delete volumes ([#29](https://github.com/ofkm/arcane/pull/29) by @kmendell)
- pull/remove images ([#33](https://github.com/ofkm/arcane/pull/33) by @kmendell)
- filter and sorting on all tables ([#35](https://github.com/ofkm/arcane/pull/35) by @kmendell)
- app version update notification ([#36](https://github.com/ofkm/arcane/pull/36) by @kmendell)

### Bug fixes

- improve container list table([13822be](https://github.com/ofkm/arcane/commit/13822befb2db7793feb75baa2e821edb53c2243d) by @kmendell)
- pull real data for dashboard([d66c735](https://github.com/ofkm/arcane/commit/d66c73540a39deddbc81a56e655573a6a2d176c8) by @kmendell)
- improve image list page([a46e249](https://github.com/ofkm/arcane/commit/a46e2493e99059ac850b0528897e9708019a7ff5) by @kmendell)
- ensure we are using consistant data tables([2e5ec48](https://github.com/ofkm/arcane/commit/2e5ec48bfb4c9861fefa3273bd919445f38de032) by @kmendell)
- type error fixes([75a2997](https://github.com/ofkm/arcane/commit/75a2997f18f4366d4c70f8fdf5025c4cd3c8af83) by @kmendell)
- dockerfile and docker socket permissions([3b1359d](https://github.com/ofkm/arcane/commit/3b1359df3841f9722a2d61216905afc208bfe84d) by @kmendell)
- use /app/data for settings and stack storage ([#18](https://github.com/ofkm/arcane/pull/18) by @kmendell)
- button size consistancy([03b636d](https://github.com/ofkm/arcane/commit/03b636dfd2e47078f848d54232edb887438afd15) by @kmendell)
- use correct github url([ee2a49c](https://github.com/ofkm/arcane/commit/ee2a49cc6959cb120a6e4d1353400228edbb4b6e) by @kmendell)

### Dependencies

- bump dockerode from 4.0.5 to 4.0.6 ([#1](https://github.com/ofkm/arcane/pull/1) by @dependabot[bot])

### Other

- initial commit([1ef5cdc](https://github.com/ofkm/arcane/commit/1ef5cdc13ba139fe82b5821919bcd83491dfc11e) by @kmendell)
- add the rest of the routes([898acf9](https://github.com/ofkm/arcane/commit/898acf97b429f3ab6b83363036e1a23855cb0a3a) by @kmendell)
- add docker connection logic([52bdcfa](https://github.com/ofkm/arcane/commit/52bdcfaa416149d0fc2f3d0cfd8b7d7eca17c2b1) by @kmendell)
- move table actions([ecf73ca](https://github.com/ofkm/arcane/commit/ecf73ca76975804553aa25aedd72954ac06ff67d) by @kmendell)
- improve container details page([46b4699](https://github.com/ofkm/arcane/commit/46b4699bd9d1fc26a79a663ef27275efb41b86e8) by @kmendell)
- improve container details page([d7d5d51](https://github.com/ofkm/arcane/commit/d7d5d510c9d560dfb29663e78f3820abbfe40b61) by @kmendell)
- add docker build files, and scripts([6505e66](https://github.com/ofkm/arcane/commit/6505e66b8466556716eeef40066b885711dfc43f) by @kmendell)
- update logo([34c6e46](https://github.com/ofkm/arcane/commit/34c6e4655bb0bab6e1bed16454f2b39650463e8a) by @kmendell)
- add issue templates([265e89c](https://github.com/ofkm/arcane/commit/265e89c26851e64f4e0cdc1a2f8df8b484aaeba7) by @kmendell)
- update release script([4680e6b](https://github.com/ofkm/arcane/commit/4680e6b39a90c6ecc9602e024f6987ce53b73112) by @kmendell)
- update release script([220df9b](https://github.com/ofkm/arcane/commit/220df9b482e9d2dd1c6a52fcb54806c263376bed) by @kmendell)

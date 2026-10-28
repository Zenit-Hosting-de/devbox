# Changelog

## [0.1.0](https://github.com/Zenit-Hosting-de/devbox/compare/devbox-v0.0.1...devbox-v0.1.0) (2026-10-28)


### Features

* **cli:** add devbox status and readable run output (AP-11, part) ([9cb0fd9](https://github.com/Zenit-Hosting-de/devbox/commit/9cb0fd9a17f3cc1f135ce192f4e01e93196e0508))
* **config:** configuration layers, placeholders and locks (AP-03) ([3bfd379](https://github.com/Zenit-Hosting-de/devbox/commit/3bfd3793235826946d30264e9dfbd2f9c8b7ce37))
* **credentials:** add devbox cred and the registry credential type ([ef2c6ec](https://github.com/Zenit-Hosting-de/devbox/commit/ef2c6ecb01d4b471b3b7b645ad9c21d97d91ae1a))
* **credentials:** add devbox cred and the registry credential type ([6157720](https://github.com/Zenit-Hosting-de/devbox/commit/61577202cf0d576ea239ab2a34149866a02d7c3a))
* **history:** record sessions with output, events, backups and retention (AP-06) ([297673c](https://github.com/Zenit-Hosting-de/devbox/commit/297673c5ea8fdc3b890e06e7aba71ba617a356fd))
* **history:** session history, output, events, backups and retention (AP-06) ([a240ffb](https://github.com/Zenit-Hosting-de/devbox/commit/a240ffb2905fd58e00c8371820880ce8c5b57ae7))
* **maven:** install Maven and manage registry credentials in settings.xml ([29a2cb9](https://github.com/Zenit-Hosting-de/devbox/commit/29a2cb96f987198134788018e1d52ee7583f33a6))
* **maven:** install Maven and write registry credentials into settings.xml ([1687281](https://github.com/Zenit-Hosting-de/devbox/commit/16872813e932173e8df9202586ef927f758e0925))
* **node:** add the node plugin ([dc28935](https://github.com/Zenit-Hosting-de/devbox/commit/dc289355cb02ab41348d3f0186107173a3885541))
* **node:** add the node plugin ([ea77968](https://github.com/Zenit-Hosting-de/devbox/commit/ea779680e385dc9fc00df3dacaa38ed57e894292))
* **npm:** add the npm consumer plugin for registry credentials ([b066276](https://github.com/Zenit-Hosting-de/devbox/commit/b0662762096ab4e6003fae2ff5f6af6639ef2491))
* **npm:** consumer plugin for registry credentials ([f020526](https://github.com/Zenit-Hosting-de/devbox/commit/f02052610727ec9005b6362b545000340db21457))
* **plugin:** plugin host with states, requirements and dependencies (AP-04) ([23de55e](https://github.com/Zenit-Hosting-de/devbox/commit/23de55ebf06e63030e661e1f97bb2df902a3e2fb))
* **plugins:** add the bundled base plugin (AP-17, part) ([2e20781](https://github.com/Zenit-Hosting-de/devbox/commit/2e20781f68e9c18599122d746f2a21825df44c43))
* **plugins:** java reference plugin and plugin tooling (AP-13) ([561dd58](https://github.com/Zenit-Hosting-de/devbox/commit/561dd58893eb2923d521b6b71964d901133a0d20))
* **runner:** plugin protocol runner with PTY, fd 3, cancel and masking (AP-05) ([81903c0](https://github.com/Zenit-Hosting-de/devbox/commit/81903c0bd8fc48dc6818594a307f3a07a75fe048))
* **sdk:** devbox sdk helper commands (AP-08) ([6841fe7](https://github.com/Zenit-Hosting-de/devbox/commit/6841fe7164d5811c5ece5d4e8c7924a9c75b28ee))
* **system:** add the system plugin and release binaries ([eaec091](https://github.com/Zenit-Hosting-de/devbox/commit/eaec091ee88259788a196f9e4ba555c41566aa7e))
* **system:** system plugin with environment loader and self-update, release binaries ([2d5a89e](https://github.com/Zenit-Hosting-de/devbox/commit/2d5a89ee940c78284a846a51a99944e4e92b59db))
* usable first state — base plugin, devbox status, readable runs, usage guide, roadmap ([0d6fdaf](https://github.com/Zenit-Hosting-de/devbox/commit/0d6fdaf887ea9e3f1cb2ddc2b895afa57ec67e5f))
* **vault:** Secret Service and age file backends with migration (AP-07) ([5c6ed73](https://github.com/Zenit-Hosting-de/devbox/commit/5c6ed73388ff717431d52de72b75acc16540bc47))
* **vault:** store secrets in the Secret Service or an age file (AP-07) ([b98dfc2](https://github.com/Zenit-Hosting-de/devbox/commit/b98dfc265e6e0919b63f819a630642468a4519e6))
* **vault:** store secrets in the Secret Service or an age file (AP-07) ([9e20ca6](https://github.com/Zenit-Hosting-de/devbox/commit/9e20ca6d4134f1598a23e429e1c3e07e4f40741e))


### Bug Fixes

* **ap16-credentials:** validate fields, check permissions on verify, skip passphrase with keyring ([32dfc9f](https://github.com/Zenit-Hosting-de/devbox/commit/32dfc9fd95f3e9160afb4bd7c64b5feec1a24d18))
* **cred:** accept plain values for array options ([9350627](https://github.com/Zenit-Hosting-de/devbox/commit/93506270df2d30b320e762f290aec5d97ba8483c))
* **maven:** accept a .sha512 file without trailing newline ([fb10ec6](https://github.com/Zenit-Hosting-de/devbox/commit/fb10ec6e3b6062c6921519751f389a5fdd1f9c87))
* **maven:** ignore commented anchors and use XML markers in settings.xml ([28ee69a](https://github.com/Zenit-Hosting-de/devbox/commit/28ee69a32d54292da471e79eaaa10d8a1ae20b2b))
* **npm:** delete only files that DevBox created on credential removal ([5a7d35d](https://github.com/Zenit-Hosting-de/devbox/commit/5a7d35d01182412e5b50d642ad9259ed7ed8f4e8))
* **system-update:** avoid SIGPIPE in release asset lookup ([439ad5b](https://github.com/Zenit-Hosting-de/devbox/commit/439ad5bfe618584346f9b71d17a918b7ba0e0c21))
* **system:** keep envLoader quiet when startup files are missing ([882128a](https://github.com/Zenit-Hosting-de/devbox/commit/882128a995431393afba9bdc0f57368ff96eb139))

## 0.0.1 (2026-10-27)


### Features

* add repository scaffold and v1 schemas (AP-01, AP-02) ([ba9e05d](https://github.com/Zenit-Hosting-de/devbox/commit/ba9e05dbcd619ca7e081e29ccd448017746b07b7))
* repository scaffold and v1 schemas (AP-01, AP-02) ([17a7b96](https://github.com/Zenit-Hosting-de/devbox/commit/17a7b96cdeca5223ca9ad6d05a4ded4877d556b8))

## Changelog

Entries are generated by release-please from Conventional Commits.

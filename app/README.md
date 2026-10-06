# Share for Android

The app to see, download and send what's on a Share server. People join with an invite (a QR
code or a link, no password) or sign in with a username and password; without an account the
app sends with a PIN, like the website. Admins manage PINs, people and Recently deleted.

Flutter draws the screens. Kotlin (`android/app/src/main/kotlin`) does what has to work without
them: the phone's key in the Android KeyStore, the choice between the local and the public
address, the transfers, and the keys of encrypted folders (`e2ee/`, see docs/e2ee-plan.md), which
the transfers and the player use to encrypt and decrypt on the phone. Downloads and uploads (in pieces, by tus or straight into the server's
S3 bucket) keep going when the app is closed and continue where they stopped. Links to a bucket
are fetched without the phone's key, which goes only to Share.

## Running it

You need Flutter 3.47 and the Android SDK (platform 37). On a phone with USB or wireless
debugging:

```sh
flutter run --flavor direct
```

There are two flavors with the same application id: `direct` is the APK the server hands out,
and `play` will be the Google Play build. Debug builds are called "Share (debug)" and install
next to a release build.

A release build needs `android/key.properties` (never committed):

```
storeFile=/path/to/share-release.jks
storePassword=…
keyAlias=…
keyPassword=…
```

Then `flutter build apk --flavor direct --release` makes
`build/app/outputs/flutter-apk/app-direct-release.apk`, the file for the server's `app.apk_file`.
Next to it, as `share.apk.json`, the server wants its version, the two parts of `version:` in
`pubspec.yaml`: `{"version_code": 100, "version_name": "0.1.0"}`. That is Share's version, from
`VERSION` (`scripts/version.sh`); CI's builds put in their own, e.g. `0.1.0-dev+abc1234`.

### Signed by GitHub Actions

CI builds the same APK with the same key, if the repository has these four secrets (Settings →
Secrets and variables → Actions):

| Secret | What it is |
|---|---|
| `ANDROID_KEYSTORE_BASE64` | `share-release.jks`, base64-encoded |
| `ANDROID_KEYSTORE_PASSWORD` | `storePassword` |
| `ANDROID_KEY_ALIAS` | `keyAlias` |
| `ANDROID_KEY_PASSWORD` | `keyPassword` |

With the [GitHub CLI](https://cli.github.com), on Windows in PowerShell:

```powershell
[Convert]::ToBase64String([IO.File]::ReadAllBytes("C:\path\to\share-release.jks")) | gh secret set ANDROID_KEYSTORE_BASE64
gh secret set ANDROID_KEYSTORE_PASSWORD   # each of these asks for the value
gh secret set ANDROID_KEY_ALIAS
gh secret set ANDROID_KEY_PASSWORD
```

On Linux or macOS the first line is `base64 < share-release.jks | gh secret set ANDROID_KEYSTORE_BASE64`.

Every push to `main`, and every pull request from one of this repository's branches, then leaves
`share.apk` and `share.apk.json` as the artifact `share-apk` for a week, and a version tag puts
them in the release. A pull request from a fork never gets the key: GitHub doesn't hand secrets to
it. Without the secrets, runs still pass without an APK, and a tag fails rather than making a
release without it.

## Tests

```sh
flutter analyze && flutter test
(cd android && ./gradlew testDirectDebugUnitTest testPlayDebugUnitTest)
flutter test --run-skipped --tags golden     # screenshots, compared with docs/share-mockup
```

The Dart tests run the screens against a fake server made from `contract/api`, with a bucket of
its own. The Kotlin unit tests send to and download from a small local server, which also plays
the bucket, and check the pinned TLS against real self-signed certificates. Both sides read
`contract/app/platform.json`, which is what they hand each other.

The golden screenshots depend on the machine's font rendering, so they are skipped by default
and not run in CI. After changing a screen, `--update-goldens` writes them again.

## Themes

`lib/ui/theme.dart` has the colours by role (`ShareColors`), and each theme gives the roles its
values (`AppTheme`): Ember (the mockups), Midnight, Moss, Plum and Black, and the light Linen
and Frost. Automatic switches between Linen and Ember with the phone. Screens only use the roles,
so a new theme is one more entry; `test/themes_golden_test.dart` shows each theme on three
screens.

## Languages and fonts

The texts are in `lib/l10n/app_{en,de,it}.arb`; `flutter pub get` generates the Dart code from
them. The notifications have their own strings in `android/app/src/main/res/values*`.

`assets/fonts` holds Noto Serif, Roboto Mono and a subset of the Lucide icons.
`tool/fonts/build.mjs` makes the subset and `lib/ui/icons.dart` from the icons the app uses.

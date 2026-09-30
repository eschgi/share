# Share for Android

The app to see and download what was sent to a Share server. People join with an invite (a QR
code or a link, no password) or sign in with a username and password.

Flutter draws the screens. Kotlin (`android/app/src/main/kotlin`) does what has to work without
them: the phone's key in the Android KeyStore, the choice between the local and the public
address, and the downloads, which keep going when the app is closed.

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

## Tests

```sh
flutter analyze && flutter test
(cd android && ./gradlew testDirectDebugUnitTest testPlayDebugUnitTest)
flutter test --run-skipped --tags golden     # screenshots, compared with docs/share-mockup
```

The Dart tests run the screens against a fake server made from `contract/api`. The Kotlin unit
tests download from a small local server and check the pinned TLS against real self-signed
certificates. Both sides read `contract/app/platform.json`, which is what they hand each other.

The golden screenshots depend on the machine's font rendering, so they are skipped by default
and not run in CI. After changing a screen, `--update-goldens` writes them again.

## Languages and fonts

The texts are in `lib/l10n/app_{en,de,it}.arb`; `flutter pub get` generates the Dart code from
them. The notifications have their own strings in `android/app/src/main/res/values*`.

`assets/fonts` holds Noto Serif, Roboto Mono and a subset of the Lucide icons.
`tool/fonts/build.mjs` makes the subset and `lib/ui/icons.dart` from the icons the app uses.

import 'package:flutter/material.dart';

import '../../app.dart';
import '../../data/platform.dart';

/// Sends the files shared into the app from other apps ("Send with Share") as soon as they are
/// there: in the signed-in app with the phone's key, in PIN sending with the PIN. [onShared]
/// brings the sending into view first.
class SharedSender extends StatefulWidget {
  const SharedSender({super.key, required this.auth, this.onShared, required this.child});
  final SendAuth auth;
  final VoidCallback? onShared;
  final Widget child;

  @override
  State<SharedSender> createState() => _SharedSenderState();
}

class _SharedSenderState extends State<SharedSender> {
  late final AppServices _services = Services.read(context);
  bool _sending = false;

  @override
  void initState() {
    super.initState();
    _services.shared.addListener(_send);
    WidgetsBinding.instance.addPostFrameCallback((_) => _send());
  }

  @override
  void dispose() {
    _services.shared.removeListener(_send);
    super.dispose();
  }

  Future<void> _send() async {
    if (_sending || !mounted || _services.shared.value == 0) return;
    _sending = true;
    widget.onShared?.call();
    try {
      await _services.platform.sendShared(widget.auth);
    } on Exception {
      // They stay waiting; the next share or start tries again.
    }
    _services.shared.value = await _services.platform.sharedCount();
    _sending = false;
  }

  @override
  Widget build(BuildContext context) => widget.child;
}

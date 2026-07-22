import 'dart:async';

import 'package:flutter/material.dart';
import 'package:mobile_scanner/mobile_scanner.dart';

import '../../data/protocol/pair_uri.dart';

/// Full-screen camera QR scanner that returns a [PairPayload] via [Navigator.pop].
class QrScanScreen extends StatefulWidget {
  const QrScanScreen({super.key});

  @override
  State<QrScanScreen> createState() => _QrScanScreenState();
}

class _QrScanScreenState extends State<QrScanScreen>
    with WidgetsBindingObserver {
  final _controller = MobileScannerController(
    detectionSpeed: DetectionSpeed.normal,
    facing: CameraFacing.back,
    formats: const [BarcodeFormat.qrCode],
  );
  bool _handled = false;
  String? _error;

  /// Last rejected payload — avoids a setState per frame at DetectionSpeed.normal.
  String? _lastRejected;
  DateTime? _lastRejectedAt;
  MobileScannerException? _scanError;
  int _restartToken = 0;

  static const _rejectThrottle = Duration(seconds: 2);

  @override
  void initState() {
    super.initState();
    // mobile_scanner only registers its own lifecycle observer when it owns the
    // controller (see MobileScanner._startScanner). We pass ours, so the camera
    // would keep streaming in the background — stop/start it ourselves.
    WidgetsBinding.instance.addObserver(this);
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _controller.dispose();
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (!_controller.value.isInitialized || _scanError != null) return;
    switch (state) {
      case AppLifecycleState.resumed:
        unawaited(_guard(_controller.start()));
      case AppLifecycleState.inactive:
      case AppLifecycleState.paused:
      case AppLifecycleState.hidden:
      case AppLifecycleState.detached:
        unawaited(_guard(_controller.stop()));
    }
  }

  /// Swallows expected controller races (already started / disposed) so they do
  /// not surface as uncaught async errors.
  Future<void> _guard(Future<void> future) async {
    try {
      await future;
    } on MobileScannerException catch (e) {
      debugPrint('QrScanScreen controller: ${e.errorCode}');
    }
  }

  void _onDetect(BarcodeCapture capture) {
    if (_handled || !mounted) return;
    for (final b in capture.barcodes) {
      final raw = b.rawValue;
      if (raw == null || raw.isEmpty) continue;
      final payload = PairPayload.tryParse(raw);
      if (payload == null) {
        _reportRejected(raw);
        continue;
      }
      _handled = true;
      Navigator.of(context).pop(payload);
      return;
    }
  }

  /// The camera reports several frames per second; only rebuild when the
  /// rejected value changes or the throttle window has elapsed.
  void _reportRejected(String raw) {
    final now = DateTime.now();
    final last = _lastRejectedAt;
    if (raw == _lastRejected &&
        last != null &&
        now.difference(last) < _rejectThrottle) {
      return;
    }
    _lastRejected = raw;
    _lastRejectedAt = now;
    setState(() => _error = 'Not an mcremote pair QR. Got: ${_short(raw)}');
  }

  String _short(String s) {
    final t = s.replaceAll('\n', ' ').trim();
    if (t.length <= 48) return t;
    return '${t.substring(0, 45)}…';
  }

  void _onScannerError(MobileScannerException error) {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (!mounted) return;
      if (_scanError?.errorCode == error.errorCode) return;
      setState(() => _scanError = error);
    });
  }

  /// Drop the failed preview and rebuild [MobileScanner] from scratch.
  Future<void> _retry() async {
    setState(() {
      _scanError = null;
      _error = null;
      _lastRejected = null;
      _lastRejectedAt = null;
      _restartToken++;
    });
    await _guard(_controller.start());
  }

  bool get _permissionDenied =>
      _scanError?.errorCode == MobileScannerErrorCode.permissionDenied;

  bool get _unsupported =>
      _scanError?.errorCode == MobileScannerErrorCode.unsupported;

  String get _errorTitle {
    if (_permissionDenied) return 'Camera permission denied';
    if (_unsupported) return 'Scanning is not supported on this device';
    return 'Camera unavailable';
  }

  String get _errorDetail {
    if (_permissionDenied) {
      return 'Grant camera access in system settings, or use Paste / Enter '
          'code on the connect screen.';
    }
    if (_unsupported) {
      return 'Use Paste or Enter code on the connect screen.';
    }
    final code = _scanError?.errorCode;
    switch (code) {
      case MobileScannerErrorCode.controllerAlreadyInitialized:
      case MobileScannerErrorCode.controllerInitializing:
        return 'The camera was still starting. Tap Retry.';
      case MobileScannerErrorCode.controllerUninitialized:
      case MobileScannerErrorCode.controllerDisposed:
        return 'The camera was released while the app was in the background. '
            'Tap Retry.';
      default:
        final detail = _scanError?.errorDetails?.message;
        return detail == null || detail.isEmpty
            ? 'Tap Retry, or use Paste / Enter code on the connect screen.'
            : detail;
    }
  }

  @override
  Widget build(BuildContext context) {
    final failed = _scanError != null;
    return Scaffold(
      appBar: AppBar(
        title: const Text('Scan pair QR'),
        actions: [
          ValueListenableBuilder<MobileScannerState>(
            valueListenable: _controller,
            builder: (context, state, _) {
              final torch = state.torchState;
              if (torch == TorchState.unavailable) {
                return const SizedBox.shrink();
              }
              return IconButton(
                tooltip: torch == TorchState.on
                    ? 'Turn torch off'
                    : 'Turn torch on',
                onPressed: failed
                    ? null
                    : () => unawaited(_guard(_controller.toggleTorch())),
                icon: Icon(
                  torch == TorchState.on ? Icons.flash_on : Icons.flash_off,
                ),
              );
            },
          ),
          TextButton(
            // Sentinel result: the connect screen opens the code sheet
            // directly instead of bouncing the user back to hunt for it.
            onPressed: () => Navigator.of(context).pop('enter_code'),
            child: const Text('Enter code'),
          ),
        ],
      ),
      body: Stack(
        fit: StackFit.expand,
        children: [
          if (failed)
            Center(
              child: Padding(
                padding: const EdgeInsets.all(24),
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    const Icon(Icons.no_photography_outlined, size: 48),
                    const SizedBox(height: 12),
                    Text(
                      _errorTitle,
                      textAlign: TextAlign.center,
                      style: Theme.of(context).textTheme.titleMedium,
                    ),
                    const SizedBox(height: 8),
                    Text(_errorDetail, textAlign: TextAlign.center),
                    const SizedBox(height: 16),
                    Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        if (!_unsupported)
                          FilledButton.icon(
                            onPressed: () => unawaited(_retry()),
                            icon: const Icon(Icons.refresh),
                            label: const Text('Retry'),
                          ),
                        const SizedBox(width: 12),
                        OutlinedButton(
                          onPressed: () => Navigator.of(context).pop(),
                          child: const Text('Go back'),
                        ),
                      ],
                    ),
                  ],
                ),
              ),
            )
          else
            MobileScanner(
              key: ValueKey<int>(_restartToken),
              controller: _controller,
              onDetect: _onDetect,
              errorBuilder: (context, error) {
                _onScannerError(error);
                return const SizedBox.shrink();
              },
            ),
          if (!failed)
            IgnorePointer(
              child: Center(
                child: Container(
                  width: 240,
                  height: 240,
                  decoration: BoxDecoration(
                    border: Border.all(color: Colors.white70, width: 2),
                    borderRadius: BorderRadius.circular(16),
                  ),
                ),
              ),
            ),
          Align(
            alignment: Alignment.bottomCenter,
            child: Container(
              width: double.infinity,
              color: Colors.black54,
              padding: const EdgeInsets.fromLTRB(16, 12, 16, 28),
              child: Text(
                _error ??
                    'Point at the QR from `mcremote pair code` on the host.',
                textAlign: TextAlign.center,
                style: const TextStyle(color: Colors.white, fontSize: 14),
              ),
            ),
          ),
        ],
      ),
    );
  }
}

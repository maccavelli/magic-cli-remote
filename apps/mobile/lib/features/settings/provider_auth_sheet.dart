/// Credential setup for one upstream (MADR 0074 D5/D11).
///
/// The form is built from the method's declared inputs rather than hard-coded
/// per vendor: eight of the thirteen upstreams in Kilo's live catalog need
/// fields beyond a key (GitHub Copilot's deployment type, GitLab's instance
/// URL, Azure's resource name), and several of those are conditional on each
/// other. Hard-coding them would go stale the first time a vendor changes.
library;

import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../../data/protocol/models.dart';
import '../widgets/vendor_icon.dart';

/// Bottom inset a sheet must leave clear so its actions are not swallowed by
/// the Android navigation/gesture bar or the keyboard.
///
/// The two are deliberately combined with max(), not sum: when the keyboard is
/// up, `viewInsets.bottom` already spans the region the navigation bar
/// occupies, so adding `viewPadding.bottom` on top would leave a visible gap.
/// When it is down, `viewInsets.bottom` is zero and only `viewPadding.bottom`
/// keeps the buttons above the system bar — which is exactly the case that was
/// broken: the submit button rendered underneath the toolbar.
double bottomInsetFor(MediaQueryData mq) =>
    math.max(mq.viewInsets.bottom, mq.viewPadding.bottom);

/// What the sheet returns when the user submits.
class ProviderAuthSubmission {
  const ProviderAuthSubmission({
    required this.method,
    required this.secret,
    required this.inputs,
  });

  final AuthMethod method;

  /// The credential. Held only for the duration of the send: the sheet clears
  /// its controller on dispose so it does not linger in the widget tree (D11).
  final String secret;
  final Map<String, String> inputs;
}

class ProviderAuthSheet extends StatefulWidget {
  const ProviderAuthSheet({
    super.key,
    required this.providerId,
    required this.upstream,
  });

  final String providerId;
  final UpstreamAuth upstream;

  @override
  State<ProviderAuthSheet> createState() => _ProviderAuthSheetState();
}

class _ProviderAuthSheetState extends State<ProviderAuthSheet> {
  AuthMethod? _method;
  final _secret = TextEditingController();
  final Map<String, TextEditingController> _text = {};
  final Map<String, String> _answers = {};
  bool _submitting = false;

  @override
  void initState() {
    super.initState();
    final usable = widget.upstream.methods.where((m) => !m.isBrowserOAuth);
    _method = usable.isNotEmpty
        ? usable.first
        : (widget.upstream.methods.isNotEmpty
              ? widget.upstream.methods.first
              : null);
    _seedDefaults();
  }

  /// Select inputs start on their first option so a conditional field has a
  /// defined answer to compare against on the first build.
  void _seedDefaults() {
    for (final input in _method?.inputs ?? const <AuthInput>[]) {
      if (input.isSelect && input.options.isNotEmpty) {
        _answers[input.key] = input.options.first.value;
      }
    }
  }

  @override
  void dispose() {
    // D11: the credential must not outlive the sheet.
    _secret.clear();
    _secret.dispose();
    for (final c in _text.values) {
      c.dispose();
    }
    super.dispose();
  }

  TextEditingController _controllerFor(String key) =>
      _text.putIfAbsent(key, TextEditingController.new);

  /// Inputs whose `when` condition is satisfied by the current answers.
  List<AuthInput> get _visibleInputs {
    final all = _method?.inputs ?? const <AuthInput>[];
    return all.where((i) => i.when?.satisfiedBy(_answers) ?? true).toList();
  }

  bool get _canSubmit {
    if (_submitting || _method == null) return false;
    if (_method!.isBrowserOAuth) return false;
    if (_method!.isApiKey && _secret.text.trim().isEmpty) return false;
    for (final input in _visibleInputs) {
      if (!input.required) continue;
      final v = input.isSelect
          ? _answers[input.key]
          : _controllerFor(input.key).text;
      if ((v ?? '').trim().isEmpty) return false;
    }
    return true;
  }

  void _submit() {
    final method = _method;
    if (method == null) return;
    final inputs = <String, String>{};
    for (final input in _visibleInputs) {
      final v = input.isSelect
          ? (_answers[input.key] ?? '')
          : _controllerFor(input.key).text.trim();
      if (v.isNotEmpty) inputs[input.key] = v;
    }
    setState(() => _submitting = true);
    final secret = _secret.text;
    // Clear before handing off: the caller owns the value from here.
    _secret.clear();
    Navigator.of(context).pop(
      ProviderAuthSubmission(method: method, secret: secret, inputs: inputs),
    );
  }

  @override
  Widget build(BuildContext context) {
    final method = _method;
    return Padding(
      padding: EdgeInsets.only(
        left: 16,
        right: 16,
        top: 16,
        bottom: bottomInsetFor(MediaQuery.of(context)) + 16,
      ),
      // Scrollable because the content can exceed the viewport on its own:
      // Azure declares three inputs plus a key field, and with the keyboard up
      // there is very little height left. Without this the submit button is
      // simply unreachable rather than merely low.
      child: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                VendorIcon(
                  id: widget.upstream.id,
                  display: widget.upstream.display,
                  size: 28,
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: Text(
                    widget.upstream.display,
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 12),
            if (widget.upstream.methods.length > 1) _methodPicker(),
            if (method == null)
              const Padding(
                padding: EdgeInsets.symmetric(vertical: 12),
                child: Text('This upstream advertises no way to sign in.'),
              )
            else ...[
              if (method.isBrowserOAuth) _browserNotice(),
              ..._visibleInputs.map(_inputField),
              if (method.isApiKey) _secretField(),
            ],
            const SizedBox(height: 16),
            Row(
              mainAxisAlignment: MainAxisAlignment.end,
              children: [
                TextButton(
                  onPressed: _submitting
                      ? null
                      : () => Navigator.of(context).pop(),
                  child: const Text('Cancel'),
                ),
                const SizedBox(width: 8),
                FilledButton(
                  key: const Key('provider-auth-submit'),
                  onPressed: _canSubmit ? _submit : null,
                  child: Text(
                    method?.isDeviceOAuth == true ? 'Start sign-in' : 'Save',
                  ),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }

  Widget _methodPicker() => Padding(
    padding: const EdgeInsets.only(bottom: 12),
    child: DropdownButtonFormField<String>(
      key: const Key('provider-auth-method'),
      initialValue: _method?.id,
      decoration: const InputDecoration(labelText: 'Method'),
      items: [
        for (final m in widget.upstream.methods)
          DropdownMenuItem(
            value: m.id,
            child: Text(m.label.isEmpty ? m.type : m.label),
          ),
      ],
      onChanged: (id) {
        final next = widget.upstream.methods.firstWhere(
          (m) => m.id == id,
          orElse: () => widget.upstream.methods.first,
        );
        setState(() {
          _method = next;
          _answers.clear();
          _seedDefaults();
        });
      },
    ),
  );

  Widget _browserNotice() => const Padding(
    padding: EdgeInsets.symmetric(vertical: 8),
    child: Text(
      'This sign-in finishes in a browser on the host itself, so it '
      'cannot be completed from the phone yet. Use a key method, or run '
      'it on the host.',
    ),
  );

  Widget _inputField(AuthInput input) {
    if (input.isSelect) {
      return Padding(
        padding: const EdgeInsets.only(bottom: 12),
        child: DropdownButtonFormField<String>(
          key: Key('provider-auth-input-${input.key}'),
          initialValue: _answers[input.key],
          decoration: InputDecoration(
            labelText: input.message?.isNotEmpty == true
                ? input.message
                : input.key,
          ),
          items: [
            for (final o in input.options)
              DropdownMenuItem(value: o.value, child: Text(o.display)),
          ],
          onChanged: (v) => setState(() => _answers[input.key] = v ?? ''),
        ),
      );
    }
    return Padding(
      padding: const EdgeInsets.only(bottom: 12),
      child: TextField(
        key: Key('provider-auth-input-${input.key}'),
        controller: _controllerFor(input.key),
        decoration: InputDecoration(
          labelText: input.message?.isNotEmpty == true
              ? input.message
              : input.key,
          hintText: input.placeholder,
        ),
        autocorrect: false,
        enableSuggestions: false,
        onChanged: (_) => setState(() {}),
      ),
    );
  }

  /// The credential field. Obscured, and with autocorrect and suggestions off
  /// so the key never reaches the keyboard's learning dictionary (D11).
  Widget _secretField() => TextField(
    key: const Key('provider-auth-secret'),
    controller: _secret,
    decoration: const InputDecoration(
      labelText: 'API key',
      hintText: 'Paste the key; it is sent to the host and not stored here',
    ),
    obscureText: true,
    autocorrect: false,
    enableSuggestions: false,
    onChanged: (_) => setState(() {}),
  );
}

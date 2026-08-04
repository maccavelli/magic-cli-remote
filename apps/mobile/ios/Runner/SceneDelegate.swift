import Flutter
import UIKit

/// iOS analogue of MainActivity's FLAG_SECURE (MADR 0067 D7): the connect
/// screen can render the device token in a revealable field, and iOS has no
/// window-level secure flag. Covering the window while the scene is not
/// active keeps the token out of the app-switcher snapshot; the cover is
/// removed the moment the scene is active again, so the user never sees it.
class SceneDelegate: FlutterSceneDelegate {
  private var privacyShield: UIView?

  override func sceneWillResignActive(_ scene: UIScene) {
    super.sceneWillResignActive(scene)
    guard let windowScene = scene as? UIWindowScene,
      let window = windowScene.windows.first(where: { $0.isKeyWindow })
        ?? windowScene.windows.first
    else { return }
    if privacyShield != nil { return }
    let shield = UIView(frame: window.bounds)
    shield.autoresizingMask = [.flexibleWidth, .flexibleHeight]
    // An opaque cover, not a blur: blurs leak layout and can rasterise
    // late, leaving a readable first snapshot frame.
    shield.backgroundColor = UIColor.systemBackground
    window.addSubview(shield)
    privacyShield = shield
  }

  override func sceneDidBecomeActive(_ scene: UIScene) {
    super.sceneDidBecomeActive(scene)
    privacyShield?.removeFromSuperview()
    privacyShield = nil
  }
}

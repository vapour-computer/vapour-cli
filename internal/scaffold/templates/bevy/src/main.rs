use bevy::prelude::*;

#[cfg(target_arch = "wasm32")]
use wasm_bindgen::prelude::*;

#[cfg(target_arch = "wasm32")]
#[wasm_bindgen(inline_js = "export function vapour_quit(){ try { if (window.vapour && typeof window.vapour.quit === 'function') window.vapour.quit(0); } catch (_) {} }")]
extern "C" {
    fn vapour_quit();
}

fn main() {
    App::new()
        .add_plugins(DefaultPlugins)
        .add_systems(Startup, setup)
        .add_systems(Last, bridge_quit)
        .run();
}

fn setup(mut commands: Commands) {
    commands.spawn(Camera2d);
}

fn bridge_quit(mut exits: MessageReader<AppExit>) {
    if exits.read().next().is_none() {
        return;
    }
    #[cfg(target_arch = "wasm32")]
    {
        vapour_quit();
    }
}

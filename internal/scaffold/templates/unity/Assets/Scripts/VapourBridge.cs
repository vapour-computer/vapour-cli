using System.Runtime.InteropServices;
using UnityEngine;

namespace Vapour
{
    public static class VapourBridge
    {
#if UNITY_WEBGL && !UNITY_EDITOR
        [DllImport("__Internal")]
        private static extern void VapourQuit();
#endif

        public static void Quit()
        {
#if UNITY_WEBGL && !UNITY_EDITOR
            try { VapourQuit(); } catch { Application.Quit(); }
#else
            Application.Quit();
#endif
        }
    }
}

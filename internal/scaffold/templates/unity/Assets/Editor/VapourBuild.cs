using System;
using System.Linq;
using UnityEditor;
using UnityEditor.Build.Reporting;
using UnityEngine;

namespace Vapour
{
    public static class Build
    {
        [MenuItem("Vapour/Build")]
        public static void Perform()
        {
            var args = Environment.GetCommandLineArgs();
            var index = Array.IndexOf(args, "-vapourOutput");
            var output = index >= 0 && index + 1 < args.Length ? args[index + 1] : "build/web";

            PlayerSettings.WebGL.compressionFormat = WebGLCompressionFormat.Brotli;
            PlayerSettings.WebGL.decompressionFallback = true;
            PlayerSettings.WebGL.nameFilesAsHashes = true;
            PlayerSettings.WebGL.template = "APPLICATION:Minimal";

            var scenes = EditorBuildSettings.scenes.Where(s => s.enabled).Select(s => s.path).ToArray();
            if (scenes.Length == 0) throw new Exception("Enable at least one scene in Build Settings.");
            var report = BuildPipeline.BuildPlayer(new BuildPlayerOptions
            {
                scenes = scenes,
                locationPathName = output,
                target = BuildTarget.WebGL,
                options = BuildOptions.None
            });
            if (report.summary.result != BuildResult.Succeeded)
                throw new Exception("Vapour WebGL export failed: " + report.summary.result);

            Debug.Log("Vapour WebGL build done (Brotli + decompression fallback + hashed names + minimal template).");
        }
    }
}

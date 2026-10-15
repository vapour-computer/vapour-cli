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
            var threads = ReadBoolArg(args, "-vapourThreads", false);
            var memoryMb = ReadIntArg(args, "-vapourMemoryMB", 0);

            PlayerSettings.WebGL.compressionFormat = WebGLCompressionFormat.Brotli;
            PlayerSettings.WebGL.decompressionFallback = true;
            PlayerSettings.WebGL.nameFilesAsHashes = true;
            PlayerSettings.WebGL.template = "APPLICATION:Minimal";
            PlayerSettings.WebGL.threadsSupport = threads;
            if (memoryMb > 0)
                PlayerSettings.WebGL.memorySize = memoryMb;

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

        private static bool ReadBoolArg(string[] args, string name, bool fallback)
        {
            var index = Array.IndexOf(args, name);
            if (index < 0 || index + 1 >= args.Length) return fallback;
            if (bool.TryParse(args[index + 1], out var parsed)) return parsed;
            return fallback;
        }

        private static int ReadIntArg(string[] args, string name, int fallback)
        {
            var index = Array.IndexOf(args, name);
            if (index < 0 || index + 1 >= args.Length) return fallback;
            if (int.TryParse(args[index + 1], out var parsed)) return parsed;
            return fallback;
        }
    }
}

package cc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnalyze(t *testing.T) {
	t.Run("handles optional cuddle arguments", func(t *testing.T) {
		op, err := Analyze([]string{"gcc", "-Ifoo", "-I", "bar"})
		require.NoError(t, err)

		assert.Equal(t, []string{"gcc", "-Ifoo", "-Ibar"}, op.Processed)
	})

	t.Run("can pickup inputs", func(t *testing.T) {
		op, err := Analyze([]string{"gcc", "-Ifoo", "-I", "bar", "qux.c"})
		require.NoError(t, err)

		assert.Equal(t, []string{"qux.c"}, op.Inputs)
	})

	t.Run("can pickup outputs", func(t *testing.T) {
		op, err := Analyze([]string{"gcc", "-Ifoo", "-Ibar", "-c", "-o", "build/qux.o", "qux.c"})
		require.NoError(t, err)

		assert.Equal(t, []string{"build/qux.o"}, op.Known["-o"])
	})

	t.Run("isn't fussed by seeing arguments it doesn't understand", func(t *testing.T) {
		op, err := Analyze([]string{"gcc", "-c", "-Os", "-Wno-error", "-pipe", "-march=native", "-I/foo/bar", "-o", "build/qux.o", "qux.c"})
		require.NoError(t, err)

		assert.Equal(t, []string{"build/qux.o"}, op.Known["-o"])
		assert.Equal(t, []string{"qux.c"}, op.Inputs)

	})

	t.Run("passes -D on to preprocess args", func(t *testing.T) {
		op, err := Analyze([]string{"gcc", "-DHAVE_OK=1", "-c", "qux.c", "-o", "qux.o"})
		require.NoError(t, err)

		assert.Equal(t, []string{"gcc", "-DHAVE_OK=1", "-E", "qux.c"}, op.PreprocessArgs())
	})

	t.Run("calculates the arguments to pass for preprocessing", func(t *testing.T) {
		op, err := Analyze([]string{
			"gcc",
			"-DHAVE_CONFIG_H",
			"-I.",
			"-I..",
			"-I../include",
			"-I../include",
			"-I/opt/iris/store/DdB4mr64a3EwosytwWnFXowrbLWVYm59xfTCT7A4AyjX-ruby-2.6.7/include",
			"-I/opt/iris/store/5kzrsXQ5YoKuZr1WLVTMLixaoByJAVdPDdy6mVz7AXcS-readline-8.0.4/include",
			"-I/opt/iris/store/CZXC7ZfA5ast2utu6HsthSPS6er8YcSwD1Mirrxso7UG-openssl-1.1.1k/include",
			"-I/opt/iris/store/B8dWiCuhzomE7nAJuzAU4wmmSnbRNdT8N9rLGLivcvSM-libyaml-0.2.4/include",
			"-isystem/opt/iris/state/include",
			"-O2",
			"-Wall",
			"-ffast-math",
			"-fsigned-char",
			"-Os",
			"-Wno-error",
			"-pipe",
			"-march=native",
			"-c",
			"framing.c",
			"-o",
			"framing.o",
		})
		require.NoError(t, err)

		assert.Equal(t, []string{
			"gcc",
			"-DHAVE_CONFIG_H",
			"-I.",
			"-I..",
			"-I../include",
			"-I../include",
			"-I/opt/iris/store/DdB4mr64a3EwosytwWnFXowrbLWVYm59xfTCT7A4AyjX-ruby-2.6.7/include",
			"-I/opt/iris/store/5kzrsXQ5YoKuZr1WLVTMLixaoByJAVdPDdy6mVz7AXcS-readline-8.0.4/include",
			"-I/opt/iris/store/CZXC7ZfA5ast2utu6HsthSPS6er8YcSwD1Mirrxso7UG-openssl-1.1.1k/include",
			"-I/opt/iris/store/B8dWiCuhzomE7nAJuzAU4wmmSnbRNdT8N9rLGLivcvSM-libyaml-0.2.4/include",
			"-isystem", "/opt/iris/state/include",
			"-O2",
			"-Wall",
			"-ffast-math",
			"-fsigned-char",
			"-Os",
			"-Wno-error",
			"-pipe",
			"-march=native",
			"-E",
			"framing.c",
		}, op.PreprocessArgs())

		assert.Equal(t, []string{
			"-DHAVE_CONFIG_H",
			"-O2",
			"-Wall",
			"-ffast-math",
			"-fsigned-char",
			"-Os",
			"-Wno-error",
			"-pipe",
			"-march=native",
		}, op.Common)
	})

	t.Run("real world tests", func(t *testing.T) {
		args := []string{"clang", "-I.", "-Iinclude",
			"-fPIC", "-arch", "x86_64", "-Os", "-Wno-error", "-pipe", "-march=nehalem", "-mmacosx-version-min=11", "-isysroot/Library/Developer/CommandLineTools/SDKs/MacOSX.sdk", "-DL_ENDIAN", "-DOPENSSL_PIC", "-DOPENSSL_CPUID_OBJ", "-DOPENSSL_IA32_SSE2", "-DOPENSSL_BN_ASM_MONT", "-DOPENSSL_BN_ASM_MONT5", "-DOPENSSL_BN_ASM_GF2m", "-DSHA1_ASM", "-DSHA256_ASM", "-DSHA512_ASM", "-DKECCAK1600_ASM", "-DRC4_ASM", "-DMD5_ASM", "-DAESNI_ASM", "-DVPAES_ASM", "-DGHASH_ASM", "-DECP_NISTZ256_ASM", "-DX25519_ASM", "-DPOLY1305_ASM", "-DOPENSSLDIR=\"/opt/iris/state/etc/openssl@1.1\"", "-DENGINESDIR=\"/opt/iris/store/9emrwLHDCpSzuABKcsn2NhQ4jUfatfxcaYhuC3UMjqQ7-openssl@1.1-1.1.1k/lib/engines-1.1\"", "-D_REENTRANT", "-DNDEBUG", "-I/opt/iris/store/C8ETcpBgqX7jdm89sND5cdRSG6ZkfE3svpzgUyVSqhjs-readline-8.0.4/include", "-I/opt/iris/store/BzGnBYESR4QQwu7M9tD6F2Hmia4PvnWcVJWQSEfq7CST-ruby-2.6.7/include", "-I/opt/iris/store/9DiD6XnmanHBbiKy56fEc2ewkPP8fXaBw5NQRViZ2CFw-libyaml-0.2.4/include", "-I/opt/iris/store/51JmNxmZSHnQ88NeBUcNcWV9ohT3L9rB22XQ6QxNAQSz-openssl-1.1.1k/include", "-isystem/opt/iris/state/include", "-isysroot/Library/Developer/CommandLineTools/SDKs/MacOSX.sdk", "-MMD", "-MF", "crypto/ec/ecp_oct.d.tmp", "-MT", "crypto/ec/ecp_oct.o", "-c", "-o", "crypto/ec/ecp_oct.o", "crypto/ec/ecp_oct.c"}
		// cmd := `clang  -I. -Iinclude -fPIC -arch x86_64 -Os -Wno-error -pipe -march=nehalem -mmacosx-version-min=11 -isysroot/Library/Developer/CommandLineTools/SDKs/MacOSX.sdk -DL_ENDIAN -DOPENSSL_PIC -DOPENSSL_CPUID_OBJ -DOPENSSL_IA32_SSE2 -DOPENSSL_BN_ASM_MONT -DOPENSSL_BN_ASM_MONT5 -DOPENSSL_BN_ASM_GF2m -DSHA1_ASM -DSHA256_ASM -DSHA512_ASM -DKECCAK1600_ASM -DRC4_ASM -DMD5_ASM -DAESNI_ASM -DVPAES_ASM -DGHASH_ASM -DECP_NISTZ256_ASM -DX25519_ASM -DPOLY1305_ASM -DOPENSSLDIR="\"/opt/iris/state/etc/openssl@1.1\"" -DENGINESDIR="\"/opt/iris/store/9emrwLHDCpSzuABKcsn2NhQ4jUfatfxcaYhuC3UMjqQ7-openssl@1.1-1.1.1k/lib/engines-1.1\"" -D_REENTRANT -DNDEBUG -I/opt/iris/store/C8ETcpBgqX7jdm89sND5cdRSG6ZkfE3svpzgUyVSqhjs-readline-8.0.4/include -I/opt/iris/store/BzGnBYESR4QQwu7M9tD6F2Hmia4PvnWcVJWQSEfq7CST-ruby-2.6.7/include -I/opt/iris/store/9DiD6XnmanHBbiKy56fEc2ewkPP8fXaBw5NQRViZ2CFw-libyaml-0.2.4/include -I/opt/iris/store/51JmNxmZSHnQ88NeBUcNcWV9ohT3L9rB22XQ6QxNAQSz-openssl-1.1.1k/include -isystem/opt/iris/state/include -isysroot/Library/Developer/CommandLineTools/SDKs/MacOSX.sdk -MMD -MF crypto/ts/ts_rsp_verify.d.tmp -MT crypto/ts/ts_rsp_verify.o -c -o crypto/ts/ts_rsp_verify.o crypto/ts/ts_rsp_verify.c`

		// args, err := shlex.Split(cmd)
		// require.NoError(t, err)

		op, err := Analyze(args)
		require.NoError(t, err)

		assert.NoError(t, op.Cachable())

		assert.Equal(t, "crypto/ec/ecp_oct.o", op.Output())

		args = []string{
			"g++-9",
			"-DCURL_STATICLIB",
			"-DLIBARCHIVE_STATIC",
			"-I/tmp/cmake-20210627-3332943-168eidz/cmake-3.20.4/Source",
			"-I/tmp/cmake-20210627-3332943-168eidz/cmake-3.20.4/Source/LexerParser",
			"-I/tmp/cmake-20210627-3332943-168eidz/cmake-3.20.4/Source/CTest",
			"-I/tmp/cmake-20210627-3332943-168eidz/cmake-3.20.4/Source/CPack",
			"-isystem",
			"/tmp/cmake-20210627-3332943-168eidz/cmake-3.20.4/Utilities/std",
			"-isystem",
			"/tmp/cmake-20210627-3332943-168eidz/cmake-3.20.4/Utilities",
			"-Os",
			"-Wno-error",
			"-pipe",
			"-march=native",
			"-O3",
			"-DNDEBUG",
			"-std=c++17",
			"-MD",
			"-MT",
			"Source/CMakeFiles/CMakeLib.dir/cmConfigureFileCommand.cxx.o",
			"-MF",
			"CMakeFiles/CMakeLib.dir/cmConfigureFileCommand.cxx.o.d",
			"-o",
			"CMakeFiles/CMakeLib.dir/cmConfigureFileCommand.cxx.o",
			"-c",
			"/tmp/cmake-20210627-3332943-168eidz/cmake-3.20.4/Source/cmConfigureFileCommand.cxx",
		}

		op, err = Analyze(args)
		require.NoError(t, err)

		assert.NoError(t, op.Cachable())

		assert.Equal(t, []string{"CMakeFiles/CMakeLib.dir/cmConfigureFileCommand.cxx.o"}, op.Outputs)
		assert.Equal(t, []string{"/tmp/cmake-20210627-3332943-168eidz/cmake-3.20.4/Source/cmConfigureFileCommand.cxx"}, op.Inputs)
	})
}

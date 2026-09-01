//go:build android

package keyring

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"github.com/AndroidGoLab/jni"
	"github.com/hkdb/flugo/pkg/bridge"
)

const (
	keystoreAlias = "icfx_keyring_key"
	keyringDir    = "keyring"
)

// --- JNI class/method cache (resolved once, reused across calls) ---

var (
	jniVM     *jni.VM
	jniVMOnce sync.Once

	jniCache     *jniMethodCache
	jniCacheOnce sync.Once
	jniCacheErr  error
)

// jniMethodCache holds all resolved JNI class and method IDs.
// Resolved once via sync.Once, then reused for every keyring operation.
// Classes are stored as *jni.Object (global refs); cast to *jni.Class via asClass().
type jniMethodCache struct {
	// java.security.KeyStore
	keystoreCls     *jni.Object
	ksGetInstance   jni.MethodID
	ksLoad          jni.MethodID
	ksContainsAlias jni.MethodID
	ksGetKey        jni.MethodID
	// javax.crypto.KeyGenerator
	keygenCls     *jni.Object
	kgGetInstance jni.MethodID
	kgInit        jni.MethodID
	kgGenerateKey jni.MethodID
	// android.security.keystore.KeyGenParameterSpec$Builder
	specBuilderCls   *jni.Object
	sbInit           jni.MethodID
	sbSetBlockModes  jni.MethodID
	sbSetEncPaddings jni.MethodID
	sbSetKeySize     jni.MethodID
	sbBuild          jni.MethodID
	// javax.crypto.Cipher
	cipherCls     *jni.Object
	ciGetInstance jni.MethodID
	ciInitEncrypt jni.MethodID
	ciInitDecrypt jni.MethodID
	ciGetIV       jni.MethodID
	ciDoFinal     jni.MethodID
	// javax.crypto.spec.GCMParameterSpec
	gcmSpecCls *jni.Object
	gcmInit    jni.MethodID
	// java.lang.String (for array creation)
	stringCls *jni.Object
}

// asClass casts a global ref *Object to *Class for JNI calls that expect a class.
func asClass(obj *jni.Object) *jni.Class {
	return (*jni.Class)(unsafe.Pointer(obj))
}

func getVM() (*jni.VM, error) {
	jniVMOnce.Do(func() {
		ptr := bridge.GetJVMPtr()
		if ptr != 0 {
			jniVM = jni.VMFromUintptr(ptr)
		}
	})
	if jniVM == nil {
		return nil, fmt.Errorf("keyring: JVM not available (%s)", bridge.JNIStatus())
	}
	return jniVM, nil
}

func getCache(env *jni.Env) (*jniMethodCache, error) {
	jniCacheOnce.Do(func() {
		jniCache, jniCacheErr = resolveJNICache(env)
	})
	return jniCache, jniCacheErr
}

func resolveJNICache(env *jni.Env) (*jniMethodCache, error) {
	c := &jniMethodCache{}
	var err error

	// java.security.KeyStore
	cls, err := env.FindClass("java/security/KeyStore")
	if err != nil {
		return nil, fmt.Errorf("FindClass(KeyStore): %w", err)
	}
	c.keystoreCls = env.NewGlobalRef(&cls.Object)
	if c.ksGetInstance, err = env.GetStaticMethodID(cls, "getInstance", "(Ljava/lang/String;)Ljava/security/KeyStore;"); err != nil {
		return nil, fmt.Errorf("KeyStore.getInstance: %w", err)
	}
	if c.ksLoad, err = env.GetMethodID(cls, "load", "(Ljava/security/KeyStore$LoadStoreParameter;)V"); err != nil {
		return nil, fmt.Errorf("KeyStore.load: %w", err)
	}
	if c.ksContainsAlias, err = env.GetMethodID(cls, "containsAlias", "(Ljava/lang/String;)Z"); err != nil {
		return nil, fmt.Errorf("KeyStore.containsAlias: %w", err)
	}
	if c.ksGetKey, err = env.GetMethodID(cls, "getKey", "(Ljava/lang/String;[C)Ljava/security/Key;"); err != nil {
		return nil, fmt.Errorf("KeyStore.getKey: %w", err)
	}

	// javax.crypto.KeyGenerator
	kgCls, err := env.FindClass("javax/crypto/KeyGenerator")
	if err != nil {
		return nil, fmt.Errorf("FindClass(KeyGenerator): %w", err)
	}
	c.keygenCls = env.NewGlobalRef(&kgCls.Object)
	if c.kgGetInstance, err = env.GetStaticMethodID(kgCls, "getInstance", "(Ljava/lang/String;Ljava/lang/String;)Ljavax/crypto/KeyGenerator;"); err != nil {
		return nil, fmt.Errorf("KeyGenerator.getInstance: %w", err)
	}
	if c.kgInit, err = env.GetMethodID(kgCls, "init", "(Ljava/security/spec/AlgorithmParameterSpec;)V"); err != nil {
		return nil, fmt.Errorf("KeyGenerator.init: %w", err)
	}
	if c.kgGenerateKey, err = env.GetMethodID(kgCls, "generateKey", "()Ljavax/crypto/SecretKey;"); err != nil {
		return nil, fmt.Errorf("KeyGenerator.generateKey: %w", err)
	}

	// android.security.keystore.KeyGenParameterSpec$Builder
	sbCls, err := env.FindClass("android/security/keystore/KeyGenParameterSpec$Builder")
	if err != nil {
		return nil, fmt.Errorf("FindClass(KeyGenParameterSpec$Builder): %w", err)
	}
	c.specBuilderCls = env.NewGlobalRef(&sbCls.Object)
	if c.sbInit, err = env.GetMethodID(sbCls, "<init>", "(Ljava/lang/String;I)V"); err != nil {
		return nil, fmt.Errorf("Builder.<init>: %w", err)
	}
	if c.sbSetBlockModes, err = env.GetMethodID(sbCls, "setBlockModes", "([Ljava/lang/String;)Landroid/security/keystore/KeyGenParameterSpec$Builder;"); err != nil {
		return nil, fmt.Errorf("Builder.setBlockModes: %w", err)
	}
	if c.sbSetEncPaddings, err = env.GetMethodID(sbCls, "setEncryptionPaddings", "([Ljava/lang/String;)Landroid/security/keystore/KeyGenParameterSpec$Builder;"); err != nil {
		return nil, fmt.Errorf("Builder.setEncryptionPaddings: %w", err)
	}
	if c.sbSetKeySize, err = env.GetMethodID(sbCls, "setKeySize", "(I)Landroid/security/keystore/KeyGenParameterSpec$Builder;"); err != nil {
		return nil, fmt.Errorf("Builder.setKeySize: %w", err)
	}
	if c.sbBuild, err = env.GetMethodID(sbCls, "build", "()Landroid/security/keystore/KeyGenParameterSpec;"); err != nil {
		return nil, fmt.Errorf("Builder.build: %w", err)
	}

	// javax.crypto.Cipher
	ciCls, err := env.FindClass("javax/crypto/Cipher")
	if err != nil {
		return nil, fmt.Errorf("FindClass(Cipher): %w", err)
	}
	c.cipherCls = env.NewGlobalRef(&ciCls.Object)
	if c.ciGetInstance, err = env.GetStaticMethodID(ciCls, "getInstance", "(Ljava/lang/String;)Ljavax/crypto/Cipher;"); err != nil {
		return nil, fmt.Errorf("Cipher.getInstance: %w", err)
	}
	if c.ciInitEncrypt, err = env.GetMethodID(ciCls, "init", "(ILjava/security/Key;)V"); err != nil {
		return nil, fmt.Errorf("Cipher.init(encrypt): %w", err)
	}
	if c.ciInitDecrypt, err = env.GetMethodID(ciCls, "init", "(ILjava/security/Key;Ljava/security/spec/AlgorithmParameterSpec;)V"); err != nil {
		return nil, fmt.Errorf("Cipher.init(decrypt): %w", err)
	}
	if c.ciGetIV, err = env.GetMethodID(ciCls, "getIV", "()[B"); err != nil {
		return nil, fmt.Errorf("Cipher.getIV: %w", err)
	}
	if c.ciDoFinal, err = env.GetMethodID(ciCls, "doFinal", "([B)[B"); err != nil {
		return nil, fmt.Errorf("Cipher.doFinal: %w", err)
	}

	// javax.crypto.spec.GCMParameterSpec
	gcmCls, err := env.FindClass("javax/crypto/spec/GCMParameterSpec")
	if err != nil {
		return nil, fmt.Errorf("FindClass(GCMParameterSpec): %w", err)
	}
	c.gcmSpecCls = env.NewGlobalRef(&gcmCls.Object)
	if c.gcmInit, err = env.GetMethodID(gcmCls, "<init>", "(I[B)V"); err != nil {
		return nil, fmt.Errorf("GCMParameterSpec.<init>: %w", err)
	}

	// java.lang.String
	strCls, err := env.FindClass("java/lang/String")
	if err != nil {
		return nil, fmt.Errorf("FindClass(String): %w", err)
	}
	c.stringCls = env.NewGlobalRef(&strCls.Object)

	return c, nil
}

// --- Keyring interface ---

type androidKeyring struct{}

func newPlatformKeyring() Keyring {
	return &androidKeyring{}
}

var (
	lastErrMu sync.Mutex
	lastErr   string
)

func LastError() string {
	lastErrMu.Lock()
	defer lastErrMu.Unlock()
	return lastErr
}

func setLastError(msg string) {
	lastErrMu.Lock()
	defer lastErrMu.Unlock()
	lastErr = msg
}

func (k *androidKeyring) Available() bool {
	setLastError("")
	v, err := getVM()
	if err != nil {
		setLastError(err.Error())
		return false
	}

	err = v.Do(func(env *jni.Env) error {
		if err := env.PushLocalFrame(16); err != nil {
			return fmt.Errorf("JNI PushLocalFrame: %w", err)
		}
		defer env.PopLocalFrame(nil)
		c, err := getCache(env)
		if err != nil {
			return fmt.Errorf("JNI cache init: %w", err)
		}
		_, err = loadKeyStore(env, c)
		return err
	})
	if err != nil {
		setLastError(fmt.Sprintf("keyring: KeyStore init: %v", err))
		return false
	}
	return true
}

func (k *androidKeyring) Set(key, value string) error {
	v, err := getVM()
	if err != nil {
		return err
	}

	return v.Do(func(env *jni.Env) error {
		// Bracket the JNI locals this op creates so they're reclaimed even on
		// the already-attached Flutter thread (where VM.Do won't detach/free).
		if err := env.PushLocalFrame(16); err != nil {
			return fmt.Errorf("JNI PushLocalFrame: %w", err)
		}
		defer env.PopLocalFrame(nil)
		c, err := getCache(env)
		if err != nil {
			return err
		}

		blob, err := encrypt(env, c, []byte(value))
		if err != nil {
			return fmt.Errorf("keyring Set(%s): %w", key, err)
		}

		dir := keyringPath()
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("keyring Set(%s): mkdir: %w", key, err)
		}

		data, err := json.Marshal(blob)
		if err != nil {
			return fmt.Errorf("keyring Set(%s): marshal: %w", key, err)
		}
		return os.WriteFile(filepath.Join(dir, key+".enc"), data, 0600)
	})
}

func (k *androidKeyring) Get(key string) (string, error) {
	v, err := getVM()
	if err != nil {
		return "", err
	}

	var result string
	err = v.Do(func(env *jni.Env) error {
		if err := env.PushLocalFrame(16); err != nil {
			return fmt.Errorf("JNI PushLocalFrame: %w", err)
		}
		defer env.PopLocalFrame(nil)
		c, err := getCache(env)
		if err != nil {
			return err
		}

		path := filepath.Join(keyringPath(), key+".enc")
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return ErrNotFound
			}
			return fmt.Errorf("keyring Get(%s): %w", key, err)
		}

		var blob encryptedBlob
		if err := json.Unmarshal(data, &blob); err != nil {
			return fmt.Errorf("keyring Get(%s): unmarshal: %w", key, err)
		}

		plaintext, err := decrypt(env, c, blob)
		if err != nil {
			return fmt.Errorf("keyring Get(%s): %w", key, err)
		}
		result = string(plaintext)
		// Best-effort: wipe the decrypted []byte now that it's copied into the
		// returned string (the string copy itself is immutable and can't be wiped).
		for i := range plaintext {
			plaintext[i] = 0
		}
		return nil
	})
	return result, err
}

func (k *androidKeyring) Delete(key string) error {
	path := filepath.Join(keyringPath(), key+".enc")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("keyring Delete(%s): %w", key, err)
	}
	return nil
}

func (k *androidKeyring) List() ([]string, error) {
	dir := keyringPath()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("keyring List: %w", err)
	}

	var keys []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".enc") {
			keys = append(keys, strings.TrimSuffix(name, ".enc"))
		}
	}
	return keys, nil
}

func keyringPath() string {
	return filepath.Join(bridge.BaseDir(), keyringDir)
}

// --- Android Keystore operations ---

type encryptedBlob struct {
	IV   string `json:"iv"`
	Data string `json:"data"`
}

// loadKeyStore opens the AndroidKeyStore and ensures our AES key exists.
func loadKeyStore(env *jni.Env, c *jniMethodCache) (*jni.Object, error) {
	jType, err := env.NewStringUTF("AndroidKeyStore")
	if err != nil {
		return nil, fmt.Errorf("NewStringUTF: %w", err)
	}

	ksObj, err := env.CallStaticObjectMethod(asClass(c.keystoreCls), c.ksGetInstance, jni.ObjectValue(&jType.Object))
	if err != nil {
		return nil, fmt.Errorf("KeyStore.getInstance: %w", err)
	}

	if err := env.CallVoidMethod(ksObj, c.ksLoad, jni.ObjectValue((*jni.Object)(nil))); err != nil {
		return nil, fmt.Errorf("KeyStore.load: %w", err)
	}

	jAlias, err := env.NewStringUTF(keystoreAlias)
	if err != nil {
		return nil, fmt.Errorf("NewStringUTF(alias): %w", err)
	}
	hasKey, err := env.CallBooleanMethod(ksObj, c.ksContainsAlias, jni.ObjectValue(&jAlias.Object))
	if err != nil {
		return nil, fmt.Errorf("containsAlias: %w", err)
	}

	if hasKey == 0 {
		if err := generateKey(env, c); err != nil {
			return nil, fmt.Errorf("generateKey: %w", err)
		}
	}

	return ksObj, nil
}

// generateKey creates a new AES-256-GCM key in the Android Keystore.
func generateKey(env *jni.Env, c *jniMethodCache) error {
	jAES, err := env.NewStringUTF("AES")
	if err != nil {
		return fmt.Errorf("NewStringUTF(AES): %w", err)
	}
	jProvider, err := env.NewStringUTF("AndroidKeyStore")
	if err != nil {
		return fmt.Errorf("NewStringUTF(provider): %w", err)
	}

	kgObj, err := env.CallStaticObjectMethod(asClass(c.keygenCls), c.kgGetInstance, jni.ObjectValue(&jAES.Object), jni.ObjectValue(&jProvider.Object))
	if err != nil {
		return fmt.Errorf("KeyGenerator.getInstance: %w", err)
	}

	// Build KeyGenParameterSpec: AES-256-GCM, no padding
	// PURPOSE_ENCRYPT | PURPOSE_DECRYPT = 1 | 2 = 3
	jAlias, err := env.NewStringUTF(keystoreAlias)
	if err != nil {
		return fmt.Errorf("NewStringUTF(alias): %w", err)
	}

	builder, err := env.NewObject(asClass(c.specBuilderCls), c.sbInit, jni.ObjectValue(&jAlias.Object), jni.IntValue(3))
	if err != nil {
		return fmt.Errorf("new Builder: %w", err)
	}

	jGCM, err := env.NewStringUTF("GCM")
	if err != nil {
		return fmt.Errorf("NewStringUTF(GCM): %w", err)
	}
	modesArr, err := env.NewObjectArray(1, asClass(c.stringCls), &jGCM.Object)
	if err != nil {
		return fmt.Errorf("NewObjectArray(modes): %w", err)
	}
	if _, err := env.CallObjectMethod(builder, c.sbSetBlockModes, jni.ObjectValue(&modesArr.Object)); err != nil {
		return fmt.Errorf("setBlockModes: %w", err)
	}

	jNoPad, err := env.NewStringUTF("NoPadding")
	if err != nil {
		return fmt.Errorf("NewStringUTF(NoPadding): %w", err)
	}
	padsArr, err := env.NewObjectArray(1, asClass(c.stringCls), &jNoPad.Object)
	if err != nil {
		return fmt.Errorf("NewObjectArray(paddings): %w", err)
	}
	if _, err := env.CallObjectMethod(builder, c.sbSetEncPaddings, jni.ObjectValue(&padsArr.Object)); err != nil {
		return fmt.Errorf("setEncryptionPaddings: %w", err)
	}

	if _, err := env.CallObjectMethod(builder, c.sbSetKeySize, jni.IntValue(256)); err != nil {
		return fmt.Errorf("setKeySize: %w", err)
	}

	spec, err := env.CallObjectMethod(builder, c.sbBuild)
	if err != nil {
		return fmt.Errorf("build: %w", err)
	}

	if err := env.CallVoidMethod(kgObj, c.kgInit, jni.ObjectValue(spec)); err != nil {
		return fmt.Errorf("KeyGenerator.init: %w", err)
	}

	if _, err := env.CallObjectMethod(kgObj, c.kgGenerateKey); err != nil {
		return fmt.Errorf("generateKey: %w", err)
	}

	return nil
}

// getKey retrieves the AES key from the Android Keystore.
func getKey(env *jni.Env, c *jniMethodCache) (*jni.Object, error) {
	jType, err := env.NewStringUTF("AndroidKeyStore")
	if err != nil {
		return nil, fmt.Errorf("NewStringUTF: %w", err)
	}

	ksObj, err := env.CallStaticObjectMethod(asClass(c.keystoreCls), c.ksGetInstance, jni.ObjectValue(&jType.Object))
	if err != nil {
		return nil, fmt.Errorf("KeyStore.getInstance: %w", err)
	}

	if err := env.CallVoidMethod(ksObj, c.ksLoad, jni.ObjectValue((*jni.Object)(nil))); err != nil {
		return nil, fmt.Errorf("KeyStore.load: %w", err)
	}

	jAlias, err := env.NewStringUTF(keystoreAlias)
	if err != nil {
		return nil, fmt.Errorf("NewStringUTF(alias): %w", err)
	}

	keyObj, err := env.CallObjectMethod(ksObj, c.ksGetKey, jni.ObjectValue(&jAlias.Object), jni.ObjectValue((*jni.Object)(nil)))
	if err != nil {
		return nil, fmt.Errorf("KeyStore.getKey: %w", err)
	}
	return keyObj, nil
}

// encrypt encrypts plaintext with AES-256-GCM using the Keystore key.
func encrypt(env *jni.Env, c *jniMethodCache, plaintext []byte) (encryptedBlob, error) {
	key, err := getKey(env, c)
	if err != nil {
		return encryptedBlob{}, fmt.Errorf("getKey: %w", err)
	}

	jTransform, err := env.NewStringUTF("AES/GCM/NoPadding")
	if err != nil {
		return encryptedBlob{}, fmt.Errorf("NewStringUTF: %w", err)
	}

	cipher, err := env.CallStaticObjectMethod(asClass(c.cipherCls), c.ciGetInstance, jni.ObjectValue(&jTransform.Object))
	if err != nil {
		return encryptedBlob{}, fmt.Errorf("Cipher.getInstance: %w", err)
	}

	// ENCRYPT_MODE = 1
	if err := env.CallVoidMethod(cipher, c.ciInitEncrypt, jni.IntValue(1), jni.ObjectValue(key)); err != nil {
		return encryptedBlob{}, fmt.Errorf("Cipher.init(ENCRYPT): %w", err)
	}

	ivObj, err := env.CallObjectMethod(cipher, c.ciGetIV)
	if err != nil {
		return encryptedBlob{}, fmt.Errorf("Cipher.getIV: %w", err)
	}
	iv := getGoBytes(env, ivObj)

	jPlaintext := goToByteArray(env, plaintext)
	encObj, err := env.CallObjectMethod(cipher, c.ciDoFinal, jni.ObjectValue(&jPlaintext.Object))
	if err != nil {
		return encryptedBlob{}, fmt.Errorf("Cipher.doFinal: %w", err)
	}

	return encryptedBlob{
		IV:   base64.StdEncoding.EncodeToString(iv),
		Data: base64.StdEncoding.EncodeToString(getGoBytes(env, encObj)),
	}, nil
}

// decrypt decrypts an encrypted blob with AES-256-GCM using the Keystore key.
func decrypt(env *jni.Env, c *jniMethodCache, blob encryptedBlob) ([]byte, error) {
	iv, err := base64.StdEncoding.DecodeString(blob.IV)
	if err != nil {
		return nil, fmt.Errorf("decode IV: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(blob.Data)
	if err != nil {
		return nil, fmt.Errorf("decode data: %w", err)
	}

	key, err := getKey(env, c)
	if err != nil {
		return nil, fmt.Errorf("getKey: %w", err)
	}

	jTransform, err := env.NewStringUTF("AES/GCM/NoPadding")
	if err != nil {
		return nil, fmt.Errorf("NewStringUTF: %w", err)
	}

	cipher, err := env.CallStaticObjectMethod(asClass(c.cipherCls), c.ciGetInstance, jni.ObjectValue(&jTransform.Object))
	if err != nil {
		return nil, fmt.Errorf("Cipher.getInstance: %w", err)
	}

	jIV := goToByteArray(env, iv)
	gcmSpec, err := env.NewObject(asClass(c.gcmSpecCls), c.gcmInit, jni.IntValue(128), jni.ObjectValue(&jIV.Object))
	if err != nil {
		return nil, fmt.Errorf("new GCMParameterSpec: %w", err)
	}

	// DECRYPT_MODE = 2
	if err := env.CallVoidMethod(cipher, c.ciInitDecrypt, jni.IntValue(2), jni.ObjectValue(key), jni.ObjectValue(gcmSpec)); err != nil {
		return nil, fmt.Errorf("Cipher.init(DECRYPT): %w", err)
	}

	jCiphertext := goToByteArray(env, ciphertext)
	decObj, err := env.CallObjectMethod(cipher, c.ciDoFinal, jni.ObjectValue(&jCiphertext.Object))
	if err != nil {
		return nil, fmt.Errorf("Cipher.doFinal: %w", err)
	}

	return getGoBytes(env, decObj), nil
}

// --- byte array helpers ---

func goToByteArray(env *jni.Env, data []byte) *jni.ByteArray {
	arr := env.NewByteArray(int32(len(data)))
	if arr == nil || len(data) == 0 {
		return arr
	}
	env.SetByteArrayRegion(arr, 0, int32(len(data)), unsafe.Pointer(&data[0]))
	return arr
}

func getGoBytes(env *jni.Env, obj *jni.Object) []byte {
	if obj == nil {
		return nil
	}
	arr := (*jni.ByteArray)(unsafe.Pointer(obj))
	length := env.GetArrayLength(&arr.Array)
	if length <= 0 {
		return nil
	}
	ptr := env.GetByteArrayElements(arr, nil)
	if ptr == nil {
		return nil
	}
	result := make([]byte, length)
	copy(result, unsafe.Slice((*byte)(ptr), length))
	env.ReleaseByteArrayElements(arr, ptr, 0)
	return result
}

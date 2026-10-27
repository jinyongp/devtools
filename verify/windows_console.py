"""Functional ConPTY execution on the native Windows host; no visual verdict."""
import ctypes
from ctypes import wintypes as w
import os
import subprocess
import threading
import time


def windows_terminal(argv, env, cwd, interrupt_after=None):
    if os.name != 'nt': raise RuntimeError('ConPTY requires native Windows')
    import msvcrt
    kernel = ctypes.WinDLL('kernel32', use_last_error=True)
    class Coord(ctypes.Structure): _fields_=[('X',w.SHORT),('Y',w.SHORT)]
    class Startup(ctypes.Structure):
        _fields_=[('cb',w.DWORD),('lpReserved',w.LPWSTR),('lpDesktop',w.LPWSTR),('lpTitle',w.LPWSTR),
                  ('dwX',w.DWORD),('dwY',w.DWORD),('dwXSize',w.DWORD),('dwYSize',w.DWORD),
                  ('dwXCountChars',w.DWORD),('dwYCountChars',w.DWORD),('dwFillAttribute',w.DWORD),
                  ('dwFlags',w.DWORD),('wShowWindow',w.WORD),('cbReserved2',w.WORD),
                  ('lpReserved2',ctypes.c_void_p),('hStdInput',w.HANDLE),('hStdOutput',w.HANDLE),('hStdError',w.HANDLE)]
    class StartupEx(ctypes.Structure): _fields_=[('StartupInfo',Startup),('lpAttributeList',ctypes.c_void_p)]
    class Process(ctypes.Structure): _fields_=[('hProcess',w.HANDLE),('hThread',w.HANDLE),('pid',w.DWORD),('tid',w.DWORD)]
    kernel.CreatePipe.argtypes=[ctypes.POINTER(w.HANDLE),ctypes.POINTER(w.HANDLE),ctypes.c_void_p,w.DWORD]
    kernel.CreatePseudoConsole.argtypes=[Coord,w.HANDLE,w.HANDLE,w.DWORD,ctypes.POINTER(w.HANDLE)]
    kernel.CreatePseudoConsole.restype=ctypes.c_long
    kernel.ClosePseudoConsole.argtypes=[w.HANDLE]
    kernel.InitializeProcThreadAttributeList.argtypes=[ctypes.c_void_p,w.DWORD,w.DWORD,ctypes.POINTER(ctypes.c_size_t)]
    kernel.UpdateProcThreadAttribute.argtypes=[ctypes.c_void_p,w.DWORD,ctypes.c_size_t,ctypes.c_void_p,ctypes.c_size_t,ctypes.c_void_p,ctypes.c_void_p]
    kernel.DeleteProcThreadAttributeList.argtypes=[ctypes.c_void_p]
    kernel.CreateProcessW.argtypes=[w.LPCWSTR,w.LPWSTR,ctypes.c_void_p,ctypes.c_void_p,w.BOOL,w.DWORD,ctypes.c_void_p,w.LPCWSTR,ctypes.POINTER(StartupEx),ctypes.POINTER(Process)]
    kernel.CloseHandle.argtypes=[w.HANDLE]
    kernel.WaitForSingleObject.argtypes=[w.HANDLE,w.DWORD]
    kernel.GetExitCodeProcess.argtypes=[w.HANDLE,ctypes.POINTER(w.DWORD)]
    kernel.TerminateProcess.argtypes=[w.HANDLE,w.UINT]
    kernel.WriteFile.argtypes=[w.HANDLE,ctypes.c_void_p,w.DWORD,ctypes.POINTER(w.DWORD),ctypes.c_void_p]
    def checked(ok):
        if not ok: raise ctypes.WinError(ctypes.get_last_error())
    incoming, writer, reader, outgoing, console = (w.HANDLE() for _ in range(5))
    process = Process(); attributes=None; output=bytearray(); read_thread=None; fd=None
    try:
        checked(kernel.CreatePipe(ctypes.byref(incoming),ctypes.byref(writer),None,0))
        checked(kernel.CreatePipe(ctypes.byref(reader),ctypes.byref(outgoing),None,0))
        if kernel.CreatePseudoConsole(Coord(120,40),incoming,outgoing,0,ctypes.byref(console)) != 0:
            raise RuntimeError('CreatePseudoConsole failed')
        size=ctypes.c_size_t()
        kernel.InitializeProcThreadAttributeList(None,1,0,ctypes.byref(size))
        attributes=ctypes.create_string_buffer(size.value)
        checked(kernel.InitializeProcThreadAttributeList(attributes,1,0,ctypes.byref(size)))
        checked(kernel.UpdateProcThreadAttribute(attributes,0,0x20016,console,ctypes.sizeof(w.HANDLE),None,None))
        startup=StartupEx();startup.StartupInfo.cb=ctypes.sizeof(StartupEx)
        # Like Windows Terminal's _LaunchAttachedClient, clear inherited stdio
        # with STARTF_USESTDHANDLES so redirected Actions handles do not bypass
        # the attached pseudoconsole's input/output channels.
        startup.StartupInfo.dwFlags=0x100
        startup.lpAttributeList=ctypes.cast(attributes,ctypes.c_void_p)
        block=ctypes.create_unicode_buffer('\0'.join(k+'='+v for k,v in sorted(env.items(),key=lambda item:item[0].upper()))+'\0\0')
        command=ctypes.create_unicode_buffer(subprocess.list2cmdline(list(map(str,argv))))
        checked(kernel.CreateProcessW(None,command,None,None,False,0x80000|0x400,block,str(cwd),ctypes.byref(startup),ctypes.byref(process)))
        kernel.CloseHandle(process.hThread);process.hThread=None
        kernel.CloseHandle(incoming);incoming=w.HANDLE()
        kernel.CloseHandle(outgoing);outgoing=w.HANDLE()
        fd=msvcrt.open_osfhandle(reader.value,os.O_RDONLY);reader=w.HANDLE()
        def collect():
            try:
                while True:
                    part=os.read(fd,65536)
                    if not part: break
                    output.extend(part)
            except OSError: pass
        read_thread=threading.Thread(target=collect,daemon=True);read_thread.start()
        if interrupt_after is not None:
            time.sleep(interrupt_after)
            assert kernel.WaitForSingleObject(process.hProcess,0)==258,'Console input ended before interrupt'
            written=w.DWORD();checked(kernel.WriteFile(writer,b'\x03',1,ctypes.byref(written),None))
        if kernel.WaitForSingleObject(process.hProcess,30000)!=0:
            raise RuntimeError('ConPTY command timed out')
        code=w.DWORD();checked(kernel.GetExitCodeProcess(process.hProcess,ctypes.byref(code)))
        kernel.ClosePseudoConsole(console);console=w.HANDLE()
        read_thread.join(timeout=5)
        if read_thread.is_alive(): raise RuntimeError('ConPTY output did not close')
        return code.value,output.decode('utf-8').replace('\r\n','\n')
    finally:
        if process.hProcess and kernel.WaitForSingleObject(process.hProcess,0)==258:
            kernel.TerminateProcess(process.hProcess,1);kernel.WaitForSingleObject(process.hProcess,5000)
        if console: kernel.ClosePseudoConsole(console)
        if fd is not None: os.close(fd)
        if read_thread is not None: read_thread.join(timeout=5)
        for handle in (incoming,writer,reader,outgoing,process.hThread,process.hProcess):
            if handle: kernel.CloseHandle(handle)
        if attributes is not None: kernel.DeleteProcThreadAttributeList(attributes)

import base64
import json
import sys
import mutagen
from mutagen.id3 import (ID3, TIT2, TPE1, TPE2, TALB, TRCK, TPOS, TDRC, TCON,
                         TCOM, COMM, TXXX, APIC)
from mutagen.mp4 import MP4, MP4Cover
from mutagen.flac import FLAC, Picture
from mutagen.wave import WAVE
from mutagen.aiff import AIFF
from mutagen.mp3 import MP3

operation, path = sys.argv[1:]
f = mutagen.File(path)
if f is None:
    raise ValueError('Unsupported audio container')
if f.tags is None and operation == 'write':
    f.add_tags()
id3 = isinstance(f, (MP3, WAVE, AIFF))
mp4 = isinstance(f, MP4)
id3keys = {'title':'TIT2','artists':'TPE1','albumArtists':'TPE2','album':'TALB',
           'track':'TRCK','disc':'TPOS','date':'TDRC','genres':'TCON','composer':'TCOM'}
vorbis = {'title':'title','artists':'artist','albumArtists':'albumartist','album':'album',
          'track':'tracknumber','disc':'discnumber','date':'date','genres':'genre',
          'composer':'composer','comment':'comment','author':'author','narrator':'narrator',
          'musicBrainzId':'musicbrainz_albumid'}
mp4keys = {'title':'\xa9nam','artists':'\xa9ART','albumArtists':'aART','album':'\xa9alb',
           'date':'\xa9day','genres':'\xa9gen','composer':'\xa9wrt','comment':'\xa9cmt'}
custom = {'author':'AUTHOR','narrator':'NARRATOR','musicBrainzId':'MusicBrainz Album Id'}
lists = {'artists','albumArtists','genres'}
classes = {'TIT2':TIT2,'TPE1':TPE1,'TPE2':TPE2,'TALB':TALB,'TRCK':TRCK,
           'TPOS':TPOS,'TDRC':TDRC,'TCON':TCON,'TCOM':TCOM}

def values(key):
    tags = f.tags
    if tags is None:
        return []
    if id3:
        if key in id3keys:
            frame = tags.get(id3keys[key])
        elif key == 'comment':
            frames = tags.getall('COMM')
            frame = frames[0] if frames else None
        else:
            frame = tags.get('TXXX:' + custom[key])
        return [str(x) for x in frame.text] if frame else []
    if mp4:
        if key in ('track','disc'):
            pair = tags.get('trkn' if key == 'track' else 'disk', [(0,0)])[0]
            return [str(pair[0]) + ('/'+str(pair[1]) if pair[1] else '')] if pair[0] else []
        name = mp4keys.get(key, '----:com.apple.iTunes:' + custom.get(key,key))
        return [v.decode('utf8') if isinstance(v,bytes) else str(v) for v in tags.get(name,[])]
    return [str(x) for x in tags.get(vorbis[key],[])]

def artwork():
    if id3:
        pics = f.tags.getall('APIC') if f.tags else []
        return pics[0].data if pics else b''
    if mp4:
        return bytes(f.tags.get('covr',[b''])[0]) if f.tags else b''
    if isinstance(f,FLAC):
        return f.pictures[0].data if f.pictures else b''
    pics = f.get('metadata_block_picture',[])
    return Picture(base64.b64decode(pics[0])).data if pics else b''

if operation == 'write':
    patch = json.load(sys.stdin)
    for key,value in patch.items():
        if key == 'artwork':
            data = base64.b64decode(value, validate=True) if value else b''
            if len(data)>600000:
                raise ValueError('Artwork too large')
            png = data.startswith(b'\x89PNG\r\n\x1a\n')
            if data and not (png or data.startswith(b'\xff\xd8\xff')):
                raise ValueError('Expected PNG or JPEG')
            mime = 'image/png' if png else 'image/jpeg'
            if id3:
                f.tags.delall('APIC')
                if data: f.tags.add(APIC(encoding=3,mime=mime,type=3,desc='',data=data))
            elif mp4:
                f.tags.pop('covr',None)
                if data: f.tags['covr']=[MP4Cover(data, imageformat=MP4Cover.FORMAT_PNG if png else MP4Cover.FORMAT_JPEG)]
            else:
                pic = Picture(); pic.data=data; pic.type=3; pic.mime=mime
                if isinstance(f,FLAC):
                    f.clear_pictures()
                    if data: f.add_picture(pic)
                else:
                    f.pop('metadata_block_picture',None)
                    if data: f['metadata_block_picture']=[base64.b64encode(pic.write()).decode('ascii')]
            continue
        if key not in vorbis:
            raise ValueError('Unknown tag')
        vals = value if isinstance(value,list) else ([value] if value else [])
        if not all(isinstance(v,str) and len(v)<=10000 for v in vals) or len(vals)>100:
            raise ValueError('Invalid tag')
        if id3:
            if key in id3keys:
                name=id3keys[key]; f.tags.delall(name)
                if vals: f.tags.add(classes[name](encoding=3,text=vals))
            elif key == 'comment':
                f.tags.delall('COMM')
                if vals: f.tags.add(COMM(encoding=3,lang='eng',desc='',text=vals))
            else:
                name=custom[key]; f.tags.delall('TXXX:'+name)
                if vals: f.tags.add(TXXX(encoding=3,desc=name,text=vals))
        elif mp4:
            name=mp4keys.get(key,'----:com.apple.iTunes:'+custom.get(key,key))
            if key in ('track','disc'):
                name='trkn' if key=='track' else 'disk'
                nums=(vals[0].split('/')+['0'])[:2] if vals else []
                vals=[tuple(int(x) for x in nums)] if nums else []
            elif name.startswith('----:'):
                vals=[v.encode('utf8') for v in vals]
            f.tags.pop(name,None)
            if vals: f.tags[name]=vals
        else:
            name=vorbis[key]; f.pop(name,None)
            if vals: f[name]=vals
    f.save()
result = {key:(values(key) if key in lists else next(iter(values(key)),'')) for key in vorbis}
print(json.dumps({'tags':result,'artwork':base64.b64encode(artwork()).decode('ascii')}))

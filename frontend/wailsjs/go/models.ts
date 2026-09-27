export namespace auth {
	
	export class AuthlibCredential {
	    Username: string;
	    ProfileName: string;
	    ProfileUuid: string;
	    AccessToken: string;
	    ApiRoot: string;
	    ServerName: string;
	
	    static createFrom(source: any = {}) {
	        return new AuthlibCredential(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Username = source["Username"];
	        this.ProfileName = source["ProfileName"];
	        this.ProfileUuid = source["ProfileUuid"];
	        this.AccessToken = source["AccessToken"];
	        this.ApiRoot = source["ApiRoot"];
	        this.ServerName = source["ServerName"];
	    }
	}
	export class AuthlibProfileInfo {
	    Id: string;
	    Name: string;
	
	    static createFrom(source: any = {}) {
	        return new AuthlibProfileInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Id = source["Id"];
	        this.Name = source["Name"];
	    }
	}
	export class AuthlibLoginResult {
	    AccessToken: string;
	    Profiles: AuthlibProfileInfo[];
	    ServerName: string;
	
	    static createFrom(source: any = {}) {
	        return new AuthlibLoginResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.AccessToken = source["AccessToken"];
	        this.Profiles = this.convertValues(source["Profiles"], AuthlibProfileInfo);
	        this.ServerName = source["ServerName"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class AuthlibServerInfo {
	    ApiRoot: string;
	    ServerName: string;
	    SkinDomains: string[];
	
	    static createFrom(source: any = {}) {
	        return new AuthlibServerInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ApiRoot = source["ApiRoot"];
	        this.ServerName = source["ServerName"];
	        this.SkinDomains = source["SkinDomains"];
	    }
	}
	export class MicrosoftAccount {
	    Username: string;
	    Uuid: string;
	    AccessToken: string;
	    RefreshToken: string;
	    XboxUserId: string;
	    ClientId: string;
	    // Go type: time
	    ExpiresAt: any;
	
	    static createFrom(source: any = {}) {
	        return new MicrosoftAccount(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Username = source["Username"];
	        this.Uuid = source["Uuid"];
	        this.AccessToken = source["AccessToken"];
	        this.RefreshToken = source["RefreshToken"];
	        this.XboxUserId = source["XboxUserId"];
	        this.ClientId = source["ClientId"];
	        this.ExpiresAt = this.convertValues(source["ExpiresAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class LaunchAccount {
	    Type: string;
	    DisplayName: string;
	    OfflineName: string;
	    OfflineSkinId: string;
	    Microsoft?: MicrosoftAccount;
	    Authlib?: AuthlibCredential;
	
	    static createFrom(source: any = {}) {
	        return new LaunchAccount(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Type = source["Type"];
	        this.DisplayName = source["DisplayName"];
	        this.OfflineName = source["OfflineName"];
	        this.OfflineSkinId = source["OfflineSkinId"];
	        this.Microsoft = this.convertValues(source["Microsoft"], MicrosoftAccount);
	        this.Authlib = this.convertValues(source["Authlib"], AuthlibCredential);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace bindings {
	
	export class JavaScanResult {
	    Found: string[];
	    Added: string[];
	    Skipped: string[];
	    Versions: string[];
	
	    static createFrom(source: any = {}) {
	        return new JavaScanResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Found = source["Found"];
	        this.Added = source["Added"];
	        this.Skipped = source["Skipped"];
	        this.Versions = source["Versions"];
	    }
	}
	export class MicrosoftBrowserLoginState {
	    Active: boolean;
	    Done: boolean;
	    Failed: boolean;
	    Percent: number;
	    Message: string;
	    ErrorText: string;
	    Username: string;
	
	    static createFrom(source: any = {}) {
	        return new MicrosoftBrowserLoginState(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Active = source["Active"];
	        this.Done = source["Done"];
	        this.Failed = source["Failed"];
	        this.Percent = source["Percent"];
	        this.Message = source["Message"];
	        this.ErrorText = source["ErrorText"];
	        this.Username = source["Username"];
	    }
	}
	export class MinecraftProfileTexture {
	    id: string;
	    url: string;
	    alias: string;
	    variant: string;
	    isActive: boolean;
	
	    static createFrom(source: any = {}) {
	        return new MinecraftProfileTexture(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.url = source["url"];
	        this.alias = source["alias"];
	        this.variant = source["variant"];
	        this.isActive = source["isActive"];
	    }
	}
	export class MinecraftProfile {
	    id: string;
	    name: string;
	    skins: MinecraftProfileTexture[];
	    capes: MinecraftProfileTexture[];
	
	    static createFrom(source: any = {}) {
	        return new MinecraftProfile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.skins = this.convertValues(source["skins"], MinecraftProfileTexture);
	        this.capes = this.convertValues(source["capes"], MinecraftProfileTexture);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class OfflineSkinChoice {
	    id: string;
	    displayName: string;
	    model: string;
	    fallbackText: string;
	    source: string;
	
	    static createFrom(source: any = {}) {
	        return new OfflineSkinChoice(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.displayName = source["displayName"];
	        this.model = source["model"];
	        this.fallbackText = source["fallbackText"];
	        this.source = source["source"];
	    }
	}
	export class PluginInfo {
	    ID: string;
	    Name: string;
	    Version: string;
	    Author: string;
	    Description: string;
	    APIVersion: string;
	    Entry: string;
	    IconFile: string;
	    Directory: string;
	    SizeBytes: number;
	    FileCount: number;
	    ModifiedAt: number;
	    Disabled: boolean;
	    Dev: boolean;
	    Capabilities: string[];
	    ManifestError: string;
	
	    static createFrom(source: any = {}) {
	        return new PluginInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Name = source["Name"];
	        this.Version = source["Version"];
	        this.Author = source["Author"];
	        this.Description = source["Description"];
	        this.APIVersion = source["APIVersion"];
	        this.Entry = source["Entry"];
	        this.IconFile = source["IconFile"];
	        this.Directory = source["Directory"];
	        this.SizeBytes = source["SizeBytes"];
	        this.FileCount = source["FileCount"];
	        this.ModifiedAt = source["ModifiedAt"];
	        this.Disabled = source["Disabled"];
	        this.Dev = source["Dev"];
	        this.Capabilities = source["Capabilities"];
	        this.ManifestError = source["ManifestError"];
	    }
	}
	export class ResourcePackFile {
	    Path: string;
	    Kind: string;
	    PngBase64: string;
	    Base64: string;
	    Text: string;
	
	    static createFrom(source: any = {}) {
	        return new ResourcePackFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Path = source["Path"];
	        this.Kind = source["Kind"];
	        this.PngBase64 = source["PngBase64"];
	        this.Base64 = source["Base64"];
	        this.Text = source["Text"];
	    }
	}
	export class ResourcePackDraft {
	    Found: boolean;
	    Name: string;
	    SavedAt: string;
	    Files: ResourcePackFile[];
	
	    static createFrom(source: any = {}) {
	        return new ResourcePackDraft(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Found = source["Found"];
	        this.Name = source["Name"];
	        this.SavedAt = source["SavedAt"];
	        this.Files = this.convertValues(source["Files"], ResourcePackFile);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class ResourcePackProject {
	    Name: string;
	    Root: string;
	    Files: ResourcePackFile[];
	
	    static createFrom(source: any = {}) {
	        return new ResourcePackProject(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Name = source["Name"];
	        this.Root = source["Root"];
	        this.Files = this.convertValues(source["Files"], ResourcePackFile);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class SkinTexture {
	    skinUri: string;
	    model: string;
	    capeUri: string;
	    displayName: string;
	    updatedAt: number;
	    cached: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SkinTexture(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.skinUri = source["skinUri"];
	        this.model = source["model"];
	        this.capeUri = source["capeUri"];
	        this.displayName = source["displayName"];
	        this.updatedAt = source["updatedAt"];
	        this.cached = source["cached"];
	    }
	}
	export class SystemFileEntry {
	    name: string;
	    path: string;
	    isDir: boolean;
	    size: number;
	    modifiedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new SystemFileEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.path = source["path"];
	        this.isDir = source["isDir"];
	        this.size = source["size"];
	        this.modifiedAt = source["modifiedAt"];
	    }
	}
	export class WallpaperEngineWallpaper {
	    Path: string;
	    Source: string;
	    Title: string;
	    Type: string;
	    Web: string;
	    Unsupported: boolean;
	
	    static createFrom(source: any = {}) {
	        return new WallpaperEngineWallpaper(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Path = source["Path"];
	        this.Source = source["Source"];
	        this.Title = source["Title"];
	        this.Type = source["Type"];
	        this.Web = source["Web"];
	        this.Unsupported = source["Unsupported"];
	    }
	}

}

export namespace config {
	
	export class GameVersionProfile {
	    MinecraftDirectory: string;
	    VersionId: string;
	    MinimumMemoryMb: number;
	    MaximumMemoryMb: number;
	    UseIndependentMemorySettings: boolean;
	    FollowGlobalAdvancedSettings: boolean;
	    WindowWidth: number;
	    WindowHeight: number;
	    IsVersionIsolationEnabled?: boolean;
	    JavaExecutable: string;
	    AdditionalJvmArguments: string[];
	    AdditionalGameArguments: string[];
	    InstanceIconOverride?: string;
	    ProcessPriority: string;
	    WrapperCommand: string;
	    AdditionalEnvironmentVariables: string[];
	    LaunchFullscreen: boolean;
	    PlaytimeSeconds: number;
	    LastPlayedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new GameVersionProfile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.MinecraftDirectory = source["MinecraftDirectory"];
	        this.VersionId = source["VersionId"];
	        this.MinimumMemoryMb = source["MinimumMemoryMb"];
	        this.MaximumMemoryMb = source["MaximumMemoryMb"];
	        this.UseIndependentMemorySettings = source["UseIndependentMemorySettings"];
	        this.FollowGlobalAdvancedSettings = source["FollowGlobalAdvancedSettings"];
	        this.WindowWidth = source["WindowWidth"];
	        this.WindowHeight = source["WindowHeight"];
	        this.IsVersionIsolationEnabled = source["IsVersionIsolationEnabled"];
	        this.JavaExecutable = source["JavaExecutable"];
	        this.AdditionalJvmArguments = source["AdditionalJvmArguments"];
	        this.AdditionalGameArguments = source["AdditionalGameArguments"];
	        this.InstanceIconOverride = source["InstanceIconOverride"];
	        this.ProcessPriority = source["ProcessPriority"];
	        this.WrapperCommand = source["WrapperCommand"];
	        this.AdditionalEnvironmentVariables = source["AdditionalEnvironmentVariables"];
	        this.LaunchFullscreen = source["LaunchFullscreen"];
	        this.PlaytimeSeconds = source["PlaytimeSeconds"];
	        this.LastPlayedAt = source["LastPlayedAt"];
	    }
	}
	export class GlobalLaunchSettings {
	    WindowWidth: number;
	    WindowHeight: number;
	    JavaExecutable: string;
	    AdditionalJvmArguments: string[];
	    AdditionalGameArguments: string[];
	    ProcessPriority: string;
	    WrapperCommand: string;
	    AdditionalEnvironmentVariables: string[];
	    LaunchFullscreen: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GlobalLaunchSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.WindowWidth = source["WindowWidth"];
	        this.WindowHeight = source["WindowHeight"];
	        this.JavaExecutable = source["JavaExecutable"];
	        this.AdditionalJvmArguments = source["AdditionalJvmArguments"];
	        this.AdditionalGameArguments = source["AdditionalGameArguments"];
	        this.ProcessPriority = source["ProcessPriority"];
	        this.WrapperCommand = source["WrapperCommand"];
	        this.AdditionalEnvironmentVariables = source["AdditionalEnvironmentVariables"];
	        this.LaunchFullscreen = source["LaunchFullscreen"];
	    }
	}
	export class JavaPathItem {
	    JavaPath: string;
	    JavaVersion: string;
	
	    static createFrom(source: any = {}) {
	        return new JavaPathItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.JavaPath = source["JavaPath"];
	        this.JavaVersion = source["JavaVersion"];
	    }
	}
	export class PlaytimeRecord {
	    MinecraftDirectory: string;
	    VersionId: string;
	    PlaytimeSeconds: number;
	    LastPlayedAt: number;
	
	    static createFrom(source: any = {}) {
	        return new PlaytimeRecord(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.MinecraftDirectory = source["MinecraftDirectory"];
	        this.VersionId = source["VersionId"];
	        this.PlaytimeSeconds = source["PlaytimeSeconds"];
	        this.LastPlayedAt = source["LastPlayedAt"];
	    }
	}

}

export namespace content {
	
	export class GameContentEntry {
	    Name: string;
	    MetadataLine: string;
	    Description: string;
	    IconPath: string;
	    FallbackGlyph: string;
	    SourcePath: string;
	    IsDisabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GameContentEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Name = source["Name"];
	        this.MetadataLine = source["MetadataLine"];
	        this.Description = source["Description"];
	        this.IconPath = source["IconPath"];
	        this.FallbackGlyph = source["FallbackGlyph"];
	        this.SourcePath = source["SourcePath"];
	        this.IsDisabled = source["IsDisabled"];
	    }
	}
	export class GameInstanceVisual {
	    IconPath: string;
	    FallbackGlyph: string;
	
	    static createFrom(source: any = {}) {
	        return new GameInstanceVisual(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.IconPath = source["IconPath"];
	        this.FallbackGlyph = source["FallbackGlyph"];
	    }
	}
	export class SaveSnapshot {
	    Id: string;
	    // Go type: time
	    CreatedAt: any;
	    Label: string;
	    Reason: string;
	    FileCount: number;
	    TotalSize: number;
	    AddedSize: number;
	    AddedFiles: number;
	    Color: string;
	    WorldName: string;
	
	    static createFrom(source: any = {}) {
	        return new SaveSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Id = source["Id"];
	        this.CreatedAt = this.convertValues(source["CreatedAt"], null);
	        this.Label = source["Label"];
	        this.Reason = source["Reason"];
	        this.FileCount = source["FileCount"];
	        this.TotalSize = source["TotalSize"];
	        this.AddedSize = source["AddedSize"];
	        this.AddedFiles = source["AddedFiles"];
	        this.Color = source["Color"];
	        this.WorldName = source["WorldName"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace download {
	
	export class ContentUpdateApplyResult {
	    TargetPath: string;
	    BackupPath: string;
	    SHA1: string;
	    SizeBytes: number;
	    Message: string;
	
	    static createFrom(source: any = {}) {
	        return new ContentUpdateApplyResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.TargetPath = source["TargetPath"];
	        this.BackupPath = source["BackupPath"];
	        this.SHA1 = source["SHA1"];
	        this.SizeBytes = source["SizeBytes"];
	        this.Message = source["Message"];
	    }
	}
	export class ModpackVerifyFile {
	    Path: string;
	    FileName: string;
	    Status: string;
	    StatusText: string;
	    DeclaredSHA1: string;
	    ActualSHA1: string;
	    Required: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ModpackVerifyFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Path = source["Path"];
	        this.FileName = source["FileName"];
	        this.Status = source["Status"];
	        this.StatusText = source["StatusText"];
	        this.DeclaredSHA1 = source["DeclaredSHA1"];
	        this.ActualSHA1 = source["ActualSHA1"];
	        this.Required = source["Required"];
	    }
	}
	export class ModpackVerifyResult {
	    Present: boolean;
	    Format: string;
	    IndexPath: string;
	    Source: string;
	    Status: string;
	    StatusText: string;
	    PackName: string;
	    PackVersion: string;
	    TotalFiles: number;
	    CurrentCount: number;
	    MissingCount: number;
	    ModifiedCount: number;
	    UnverifiableCount: number;
	    Files: ModpackVerifyFile[];
	
	    static createFrom(source: any = {}) {
	        return new ModpackVerifyResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Present = source["Present"];
	        this.Format = source["Format"];
	        this.IndexPath = source["IndexPath"];
	        this.Source = source["Source"];
	        this.Status = source["Status"];
	        this.StatusText = source["StatusText"];
	        this.PackName = source["PackName"];
	        this.PackVersion = source["PackVersion"];
	        this.TotalFiles = source["TotalFiles"];
	        this.CurrentCount = source["CurrentCount"];
	        this.MissingCount = source["MissingCount"];
	        this.ModifiedCount = source["ModifiedCount"];
	        this.UnverifiableCount = source["UnverifiableCount"];
	        this.Files = this.convertValues(source["Files"], ModpackVerifyFile);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ContentUpdateFile {
	    FilePath: string;
	    FileName: string;
	    Kind: string;
	    KindLabel: string;
	    Status: string;
	    StatusText: string;
	    SHA1: string;
	    SizeBytes: number;
	    SizeText: string;
	    ProjectID: string;
	    ProjectName: string;
	    CurrentVersion: string;
	    CurrentVersionID: string;
	    LatestVersion: string;
	    LatestVersionID: string;
	    LatestFileName: string;
	    DownloadURL: string;
	    DownloadSizeBytes: number;
	    DownloadSizeText: string;
	    ReleaseDate: string;
	    CompatibleWithInstance: boolean;
	    GameVersionsText: string;
	    LoadersText: string;
	
	    static createFrom(source: any = {}) {
	        return new ContentUpdateFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.FilePath = source["FilePath"];
	        this.FileName = source["FileName"];
	        this.Kind = source["Kind"];
	        this.KindLabel = source["KindLabel"];
	        this.Status = source["Status"];
	        this.StatusText = source["StatusText"];
	        this.SHA1 = source["SHA1"];
	        this.SizeBytes = source["SizeBytes"];
	        this.SizeText = source["SizeText"];
	        this.ProjectID = source["ProjectID"];
	        this.ProjectName = source["ProjectName"];
	        this.CurrentVersion = source["CurrentVersion"];
	        this.CurrentVersionID = source["CurrentVersionID"];
	        this.LatestVersion = source["LatestVersion"];
	        this.LatestVersionID = source["LatestVersionID"];
	        this.LatestFileName = source["LatestFileName"];
	        this.DownloadURL = source["DownloadURL"];
	        this.DownloadSizeBytes = source["DownloadSizeBytes"];
	        this.DownloadSizeText = source["DownloadSizeText"];
	        this.ReleaseDate = source["ReleaseDate"];
	        this.CompatibleWithInstance = source["CompatibleWithInstance"];
	        this.GameVersionsText = source["GameVersionsText"];
	        this.LoadersText = source["LoadersText"];
	    }
	}
	export class ContentUpdateCheckResult {
	    VersionID: string;
	    ContentDirectory: string;
	    ModsDirectory: string;
	    ResourcePacksDirectory: string;
	    ShaderPacksDirectory: string;
	    GameVersion: string;
	    LoaderName: string;
	    Files: ContentUpdateFile[];
	    UpdatableCount: number;
	    LatestCount: number;
	    UnknownCount: number;
	    FailedCount: number;
	    SkippedCount: number;
	    CheckedFileCount: number;
	    DuplicateFileCount: number;
	    HashRequestCount: number;
	    HashBatchSize: number;
	    Modpack: ModpackVerifyResult;
	    Notices: string[];
	
	    static createFrom(source: any = {}) {
	        return new ContentUpdateCheckResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.VersionID = source["VersionID"];
	        this.ContentDirectory = source["ContentDirectory"];
	        this.ModsDirectory = source["ModsDirectory"];
	        this.ResourcePacksDirectory = source["ResourcePacksDirectory"];
	        this.ShaderPacksDirectory = source["ShaderPacksDirectory"];
	        this.GameVersion = source["GameVersion"];
	        this.LoaderName = source["LoaderName"];
	        this.Files = this.convertValues(source["Files"], ContentUpdateFile);
	        this.UpdatableCount = source["UpdatableCount"];
	        this.LatestCount = source["LatestCount"];
	        this.UnknownCount = source["UnknownCount"];
	        this.FailedCount = source["FailedCount"];
	        this.SkippedCount = source["SkippedCount"];
	        this.CheckedFileCount = source["CheckedFileCount"];
	        this.DuplicateFileCount = source["DuplicateFileCount"];
	        this.HashRequestCount = source["HashRequestCount"];
	        this.HashBatchSize = source["HashBatchSize"];
	        this.Modpack = this.convertValues(source["Modpack"], ModpackVerifyResult);
	        this.Notices = source["Notices"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class ContentVersionOptions {
	    FilePath: string;
	    FileName: string;
	    ProjectID: string;
	    ProjectName: string;
	    ProjectPageURL: string;
	    Source: string;
	    CurrentVersionID: string;
	    CurrentVersionNumber: string;
	    CurrentSHA1: string;
	    Versions: models.ResourceVersion[];
	    MatchedCount: number;
	    Notice: string;
	
	    static createFrom(source: any = {}) {
	        return new ContentVersionOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.FilePath = source["FilePath"];
	        this.FileName = source["FileName"];
	        this.ProjectID = source["ProjectID"];
	        this.ProjectName = source["ProjectName"];
	        this.ProjectPageURL = source["ProjectPageURL"];
	        this.Source = source["Source"];
	        this.CurrentVersionID = source["CurrentVersionID"];
	        this.CurrentVersionNumber = source["CurrentVersionNumber"];
	        this.CurrentSHA1 = source["CurrentSHA1"];
	        this.Versions = this.convertValues(source["Versions"], models.ResourceVersion);
	        this.MatchedCount = source["MatchedCount"];
	        this.Notice = source["Notice"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class DownloadSource {
	    Name: string;
	    LauncherMeta: string;
	    Meta: string;
	    Libraries: string;
	    Resources: string;
	    Maven: string;
	
	    static createFrom(source: any = {}) {
	        return new DownloadSource(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Name = source["Name"];
	        this.LauncherMeta = source["LauncherMeta"];
	        this.Meta = source["Meta"];
	        this.Libraries = source["Libraries"];
	        this.Resources = source["Resources"];
	        this.Maven = source["Maven"];
	    }
	}
	export class GameDownloadSnapshot {
	    Revision: number;
	    TaskID: number;
	    Phase: number;
	    VersionID: string;
	    StageIndex: number;
	    StageName: string;
	    Detail: string;
	    Percentage: number;
	    CompletedBytes: number;
	    TotalBytes: number;
	    CompletedFiles: number;
	    TotalFiles: number;
	    BytesPerSecond: number;
	
	    static createFrom(source: any = {}) {
	        return new GameDownloadSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Revision = source["Revision"];
	        this.TaskID = source["TaskID"];
	        this.Phase = source["Phase"];
	        this.VersionID = source["VersionID"];
	        this.StageIndex = source["StageIndex"];
	        this.StageName = source["StageName"];
	        this.Detail = source["Detail"];
	        this.Percentage = source["Percentage"];
	        this.CompletedBytes = source["CompletedBytes"];
	        this.TotalBytes = source["TotalBytes"];
	        this.CompletedFiles = source["CompletedFiles"];
	        this.TotalFiles = source["TotalFiles"];
	        this.BytesPerSecond = source["BytesPerSecond"];
	    }
	}
	export class InstalledJavaRuntime {
	    DirectoryPath: string;
	    JavaExecutablePath: string;
	    MajorVersion?: number;
	    Vendor?: number;
	
	    static createFrom(source: any = {}) {
	        return new InstalledJavaRuntime(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.DirectoryPath = source["DirectoryPath"];
	        this.JavaExecutablePath = source["JavaExecutablePath"];
	        this.MajorVersion = source["MajorVersion"];
	        this.Vendor = source["Vendor"];
	    }
	}
	export class JavaDownloadCandidate {
	    Vendor: number;
	    MajorVersion: number;
	    BuildVersion: string;
	    DownloadURL: string;
	    SHA256: string;
	    SizeBytes: number;
	
	    static createFrom(source: any = {}) {
	        return new JavaDownloadCandidate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Vendor = source["Vendor"];
	        this.MajorVersion = source["MajorVersion"];
	        this.BuildVersion = source["BuildVersion"];
	        this.DownloadURL = source["DownloadURL"];
	        this.SHA256 = source["SHA256"];
	        this.SizeBytes = source["SizeBytes"];
	    }
	}
	export class ModLoaderVersion {
	    Type: number;
	    LoaderVersion: string;
	    IsStable: boolean;
	    BuildNumber: number;
	    MetadataURL: string;
	    RequiresInstallerExtraction: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ModLoaderVersion(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Type = source["Type"];
	        this.LoaderVersion = source["LoaderVersion"];
	        this.IsStable = source["IsStable"];
	        this.BuildNumber = source["BuildNumber"];
	        this.MetadataURL = source["MetadataURL"];
	        this.RequiresInstallerExtraction = source["RequiresInstallerExtraction"];
	    }
	}
	export class ModpackInstallResult {
	    InstalledFiles: number;
	    DownloadedMods: number;
	    Errors: string[];
	
	    static createFrom(source: any = {}) {
	        return new ModpackInstallResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.InstalledFiles = source["InstalledFiles"];
	        this.DownloadedMods = source["DownloadedMods"];
	        this.Errors = source["Errors"];
	    }
	}
	export class ModpackRequirements {
	    MinecraftVersion: string;
	    LoaderType: number;
	    LoaderVersion: string;
	    RawLoaderKey: string;
	
	    static createFrom(source: any = {}) {
	        return new ModpackRequirements(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.MinecraftVersion = source["MinecraftVersion"];
	        this.LoaderType = source["LoaderType"];
	        this.LoaderVersion = source["LoaderVersion"];
	        this.RawLoaderKey = source["RawLoaderKey"];
	    }
	}
	
	
	export class SourceLatency {
	    Name: string;
	    LatencyMs: number;
	    Available: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SourceLatency(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Name = source["Name"];
	        this.LatencyMs = source["LatencyMs"];
	        this.Available = source["Available"];
	    }
	}

}

export namespace instance {
	
	export class ExternalGameInstanceLayout {
	    InstanceId: string;
	    InstanceDirectory: string;
	    ContentDirectory: string;
	    LauncherRoot: string;
	    Provider: string;
	    Evidence: string;
	
	    static createFrom(source: any = {}) {
	        return new ExternalGameInstanceLayout(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.InstanceId = source["InstanceId"];
	        this.InstanceDirectory = source["InstanceDirectory"];
	        this.ContentDirectory = source["ContentDirectory"];
	        this.LauncherRoot = source["LauncherRoot"];
	        this.Provider = source["Provider"];
	        this.Evidence = source["Evidence"];
	    }
	}
	export class GameInstanceSnapshot {
	    SourcePath: string;
	    MinecraftDirectory: string;
	    GameDirectory: string;
	    VersionIds: string[];
	    SelectedVersionId: string;
	    UsesVersionDirectoryAsGameDirectory: boolean;
	    IsLoading: boolean;
	    ErrorMessage: string;
	
	    static createFrom(source: any = {}) {
	        return new GameInstanceSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.SourcePath = source["SourcePath"];
	        this.MinecraftDirectory = source["MinecraftDirectory"];
	        this.GameDirectory = source["GameDirectory"];
	        this.VersionIds = source["VersionIds"];
	        this.SelectedVersionId = source["SelectedVersionId"];
	        this.UsesVersionDirectoryAsGameDirectory = source["UsesVersionDirectoryAsGameDirectory"];
	        this.IsLoading = source["IsLoading"];
	        this.ErrorMessage = source["ErrorMessage"];
	    }
	}
	export class GameVersionDetails {
	    VersionId: string;
	    VersionDirectory: string;
	    ContentDirectory: string;
	    LayoutProvider: string;
	    LayoutEvidence: string;
	    IsIsolated: boolean;
	    IsExternallyManaged: boolean;
	    VersionType: string;
	    BaseGameVersion: string;
	    LoaderName: string;
	    LoaderVersion: string;
	    InstanceIconPath: string;
	    InstanceIconGlyph: string;
	    ReleaseTime: string;
	    MainClass: string;
	    JavaRequirement: string;
	    Mods: content.GameContentEntry[];
	    ResourcePacks: content.GameContentEntry[];
	    Shaders: content.GameContentEntry[];
	    Saves: content.GameContentEntry[];
	    HasShaderDirectory: boolean;
	
	    static createFrom(source: any = {}) {
	        return new GameVersionDetails(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.VersionId = source["VersionId"];
	        this.VersionDirectory = source["VersionDirectory"];
	        this.ContentDirectory = source["ContentDirectory"];
	        this.LayoutProvider = source["LayoutProvider"];
	        this.LayoutEvidence = source["LayoutEvidence"];
	        this.IsIsolated = source["IsIsolated"];
	        this.IsExternallyManaged = source["IsExternallyManaged"];
	        this.VersionType = source["VersionType"];
	        this.BaseGameVersion = source["BaseGameVersion"];
	        this.LoaderName = source["LoaderName"];
	        this.LoaderVersion = source["LoaderVersion"];
	        this.InstanceIconPath = source["InstanceIconPath"];
	        this.InstanceIconGlyph = source["InstanceIconGlyph"];
	        this.ReleaseTime = source["ReleaseTime"];
	        this.MainClass = source["MainClass"];
	        this.JavaRequirement = source["JavaRequirement"];
	        this.Mods = this.convertValues(source["Mods"], content.GameContentEntry);
	        this.ResourcePacks = this.convertValues(source["ResourcePacks"], content.GameContentEntry);
	        this.Shaders = this.convertValues(source["Shaders"], content.GameContentEntry);
	        this.Saves = this.convertValues(source["Saves"], content.GameContentEntry);
	        this.HasShaderDirectory = source["HasShaderDirectory"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class GameVersionLayout {
	    IsIsolated: boolean;
	    ContentDirectory: string;
	    Provider: string;
	    Evidence: string;
	
	    static createFrom(source: any = {}) {
	        return new GameVersionLayout(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.IsIsolated = source["IsIsolated"];
	        this.ContentDirectory = source["ContentDirectory"];
	        this.Provider = source["Provider"];
	        this.Evidence = source["Evidence"];
	    }
	}
	export class ImportableInstance {
	    Provider: string;
	    Name: string;
	    Path: string;
	    ContentDirectory: string;
	    GameVersion: string;
	    Registered: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ImportableInstance(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Provider = source["Provider"];
	        this.Name = source["Name"];
	        this.Path = source["Path"];
	        this.ContentDirectory = source["ContentDirectory"];
	        this.GameVersion = source["GameVersion"];
	        this.Registered = source["Registered"];
	    }
	}
	export class ScreenshotInfo {
	    Path: string;
	    Name: string;
	    SizeBytes: number;
	    // Go type: time
	    ModifiedAt: any;
	
	    static createFrom(source: any = {}) {
	        return new ScreenshotInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Path = source["Path"];
	        this.Name = source["Name"];
	        this.SizeBytes = source["SizeBytes"];
	        this.ModifiedAt = this.convertValues(source["ModifiedAt"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace launch {
	
	export class CrashDiagnosis {
	    ReportPath: string;
	    Description: string;
	    Exception: string;
	    Suspected: string[];
	    Suggestions: string[];
	    Summary: string;
	
	    static createFrom(source: any = {}) {
	        return new CrashDiagnosis(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ReportPath = source["ReportPath"];
	        this.Description = source["Description"];
	        this.Exception = source["Exception"];
	        this.Suspected = source["Suspected"];
	        this.Suggestions = source["Suggestions"];
	        this.Summary = source["Summary"];
	    }
	}
	export class GameLaunchSnapshot {
	    Revision: number;
	    Phase: number;
	    Title: string;
	    Message: string;
	    VersionId: string;
	    AccountName: string;
	    ProcessId: number;
	
	    static createFrom(source: any = {}) {
	        return new GameLaunchSnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Revision = source["Revision"];
	        this.Phase = source["Phase"];
	        this.Title = source["Title"];
	        this.Message = source["Message"];
	        this.VersionId = source["VersionId"];
	        this.AccountName = source["AccountName"];
	        this.ProcessId = source["ProcessId"];
	    }
	}
	export class LaunchResult {
	    Success: boolean;
	    Message: string;
	
	    static createFrom(source: any = {}) {
	        return new LaunchResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Success = source["Success"];
	        this.Message = source["Message"];
	    }
	}
	export class SystemMemorySnapshot {
	    TotalMemoryMb: number;
	    AvailableMemoryMb: number;
	
	    static createFrom(source: any = {}) {
	        return new SystemMemorySnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.TotalMemoryMb = source["TotalMemoryMb"];
	        this.AvailableMemoryMb = source["AvailableMemoryMb"];
	    }
	}

}

export namespace mcserver {
	
	export class BackupInfo {
	    Name: string;
	    Path: string;
	    SizeBytes: number;
	    CreatedAt: number;
	    Hot: boolean;
	
	    static createFrom(source: any = {}) {
	        return new BackupInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Name = source["Name"];
	        this.Path = source["Path"];
	        this.SizeBytes = source["SizeBytes"];
	        this.CreatedAt = source["CreatedAt"];
	        this.Hot = source["Hot"];
	    }
	}
	export class BackupSettings {
	    Enabled: boolean;
	    IntervalHours: number;
	    KeepCount: number;
	    KeepDays: number;
	
	    static createFrom(source: any = {}) {
	        return new BackupSettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Enabled = source["Enabled"];
	        this.IntervalHours = source["IntervalHours"];
	        this.KeepCount = source["KeepCount"];
	        this.KeepDays = source["KeepDays"];
	    }
	}
	export class BannedPlayerEntry {
	    uuid: string;
	    name: string;
	    created: string;
	    source: string;
	    expires: string;
	    reason: string;
	
	    static createFrom(source: any = {}) {
	        return new BannedPlayerEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.uuid = source["uuid"];
	        this.name = source["name"];
	        this.created = source["created"];
	        this.source = source["source"];
	        this.expires = source["expires"];
	        this.reason = source["reason"];
	    }
	}
	export class CreateOptions {
	    Name: string;
	    Core: string;
	    MCVersion: string;
	    CoreVersion: string;
	    Port: number;
	    MaxPlayers: number;
	    JavaPath: string;
	    AcceptEULA: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CreateOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Name = source["Name"];
	        this.Core = source["Core"];
	        this.MCVersion = source["MCVersion"];
	        this.CoreVersion = source["CoreVersion"];
	        this.Port = source["Port"];
	        this.MaxPlayers = source["MaxPlayers"];
	        this.JavaPath = source["JavaPath"];
	        this.AcceptEULA = source["AcceptEULA"];
	    }
	}
	export class FileEntry {
	    Name: string;
	    IsDir: boolean;
	    Size: number;
	    // Go type: time
	    ModTime: any;
	
	    static createFrom(source: any = {}) {
	        return new FileEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Name = source["Name"];
	        this.IsDir = source["IsDir"];
	        this.Size = source["Size"];
	        this.ModTime = this.convertValues(source["ModTime"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class LaunchOptions {
	    MemoryMB: number;
	    MemoryMinMB: number;
	    ExtraJavaArgs: string[];
	
	    static createFrom(source: any = {}) {
	        return new LaunchOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.MemoryMB = source["MemoryMB"];
	        this.MemoryMinMB = source["MemoryMinMB"];
	        this.ExtraJavaArgs = source["ExtraJavaArgs"];
	    }
	}
	export class OpEntry {
	    uuid: string;
	    name: string;
	    level: number;
	    bypassesPlayerLimit: boolean;
	
	    static createFrom(source: any = {}) {
	        return new OpEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.uuid = source["uuid"];
	        this.name = source["name"];
	        this.level = source["level"];
	        this.bypassesPlayerLimit = source["bypassesPlayerLimit"];
	    }
	}
	export class Property {
	    Key: string;
	    Value: string;
	
	    static createFrom(source: any = {}) {
	        return new Property(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Key = source["Key"];
	        this.Value = source["Value"];
	    }
	}
	export class ServerContentEntry {
	    Name: string;
	    FileName: string;
	    SizeBytes: number;
	    Enabled: boolean;
	    Path: string;
	
	    static createFrom(source: any = {}) {
	        return new ServerContentEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Name = source["Name"];
	        this.FileName = source["FileName"];
	        this.SizeBytes = source["SizeBytes"];
	        this.Enabled = source["Enabled"];
	        this.Path = source["Path"];
	    }
	}
	export class ServerInfo {
	    ID: string;
	    Name: string;
	    Core: string;
	    CoreVersion: string;
	    MCVersion: string;
	    Port: number;
	    MaxPlayers: number;
	    Status: string;
	    Players: number;
	
	    static createFrom(source: any = {}) {
	        return new ServerInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Name = source["Name"];
	        this.Core = source["Core"];
	        this.CoreVersion = source["CoreVersion"];
	        this.MCVersion = source["MCVersion"];
	        this.Port = source["Port"];
	        this.MaxPlayers = source["MaxPlayers"];
	        this.Status = source["Status"];
	        this.Players = source["Players"];
	    }
	}
	export class WhitelistEntry {
	    uuid: string;
	    name: string;
	
	    static createFrom(source: any = {}) {
	        return new WhitelistEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.uuid = source["uuid"];
	        this.name = source["name"];
	    }
	}
	export class ServerPlayers {
	    Online: string[];
	    WhitelistEnabled: boolean;
	    Whitelist: WhitelistEntry[];
	    Ops: OpEntry[];
	    Banned: BannedPlayerEntry[];
	    Running: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ServerPlayers(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Online = source["Online"];
	        this.WhitelistEnabled = source["WhitelistEnabled"];
	        this.Whitelist = this.convertValues(source["Whitelist"], WhitelistEntry);
	        this.Ops = this.convertValues(source["Ops"], OpEntry);
	        this.Banned = this.convertValues(source["Banned"], BannedPlayerEntry);
	        this.Running = source["Running"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Snapshot {
	    Status: string;
	    Players: number;
	    CPUPercent: number;
	    MemoryMB: number;
	    LogLines: string[];
	    NextCursor: number;
	
	    static createFrom(source: any = {}) {
	        return new Snapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Status = source["Status"];
	        this.Players = source["Players"];
	        this.CPUPercent = source["CPUPercent"];
	        this.MemoryMB = source["MemoryMB"];
	        this.LogLines = source["LogLines"];
	        this.NextCursor = source["NextCursor"];
	    }
	}

}

export namespace models {
	
	export class MinecraftVersion {
	    id: string;
	    type: string;
	    url: string;
	    // Go type: time
	    time: any;
	    // Go type: time
	    releaseTime: any;
	
	    static createFrom(source: any = {}) {
	        return new MinecraftVersion(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.type = source["type"];
	        this.url = source["url"];
	        this.time = this.convertValues(source["time"], null);
	        this.releaseTime = this.convertValues(source["releaseTime"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ModrinthProject {
	    project_id: string;
	    title: string;
	    description: string;
	    icon_url: string;
	    project_type: string;
	    downloads: number;
	    follows: number;
	    slug: string;
	    versions: string[];
	    // Go type: time
	    date_created: any;
	
	    static createFrom(source: any = {}) {
	        return new ModrinthProject(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.project_id = source["project_id"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.icon_url = source["icon_url"];
	        this.project_type = source["project_type"];
	        this.downloads = source["downloads"];
	        this.follows = source["follows"];
	        this.slug = source["slug"];
	        this.versions = source["versions"];
	        this.date_created = this.convertValues(source["date_created"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ResourceDependency {
	    projectId: string;
	    versionId: string;
	    fileName: string;
	    kind: string;
	    kindDisplay: string;
	    required: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ResourceDependency(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.projectId = source["projectId"];
	        this.versionId = source["versionId"];
	        this.fileName = source["fileName"];
	        this.kind = source["kind"];
	        this.kindDisplay = source["kindDisplay"];
	        this.required = source["required"];
	    }
	}
	export class ResourceDownloadRequest {
	    source: string;
	    projectId: string;
	    versionId: string;
	    contentDirectory: string;
	    subDirectory: string;
	
	    static createFrom(source: any = {}) {
	        return new ResourceDownloadRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = source["source"];
	        this.projectId = source["projectId"];
	        this.versionId = source["versionId"];
	        this.contentDirectory = source["contentDirectory"];
	        this.subDirectory = source["subDirectory"];
	    }
	}
	export class ResourceDownloadResult {
	    savedPath: string;
	    fileName: string;
	    fileSize: number;
	    sourceUrl: string;
	    usedFallback: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ResourceDownloadResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.savedPath = source["savedPath"];
	        this.fileName = source["fileName"];
	        this.fileSize = source["fileSize"];
	        this.sourceUrl = source["sourceUrl"];
	        this.usedFallback = source["usedFallback"];
	    }
	}
	export class ResourceHit {
	    source: string;
	    projectId: string;
	    slug: string;
	    title: string;
	    description: string;
	    author: string;
	    iconUrl: string;
	    projectType: string;
	    pageUrl: string;
	    downloads: number;
	    follows: number;
	    downloadsDisplay: string;
	    followsDisplay: string;
	    typeDisplay: string;
	    typeIcon: string;
	    dateDisplay: string;
	
	    static createFrom(source: any = {}) {
	        return new ResourceHit(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = source["source"];
	        this.projectId = source["projectId"];
	        this.slug = source["slug"];
	        this.title = source["title"];
	        this.description = source["description"];
	        this.author = source["author"];
	        this.iconUrl = source["iconUrl"];
	        this.projectType = source["projectType"];
	        this.pageUrl = source["pageUrl"];
	        this.downloads = source["downloads"];
	        this.follows = source["follows"];
	        this.downloadsDisplay = source["downloadsDisplay"];
	        this.followsDisplay = source["followsDisplay"];
	        this.typeDisplay = source["typeDisplay"];
	        this.typeIcon = source["typeIcon"];
	        this.dateDisplay = source["dateDisplay"];
	    }
	}
	export class ResourceSearchRequest {
	    source: string;
	    projectType: string;
	    query: string;
	    gameVersion: string;
	    loader: string;
	    loaders: string[];
	    limit: number;
	
	    static createFrom(source: any = {}) {
	        return new ResourceSearchRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = source["source"];
	        this.projectType = source["projectType"];
	        this.query = source["query"];
	        this.gameVersion = source["gameVersion"];
	        this.loader = source["loader"];
	        this.loaders = source["loaders"];
	        this.limit = source["limit"];
	    }
	}
	export class ResourceSearchResult {
	    source: string;
	    hits: ResourceHit[];
	    total: number;
	    message: string;
	    needsApiKey: boolean;
	    usedMirror: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ResourceSearchResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = source["source"];
	        this.hits = this.convertValues(source["hits"], ResourceHit);
	        this.total = source["total"];
	        this.message = source["message"];
	        this.needsApiKey = source["needsApiKey"];
	        this.usedMirror = source["usedMirror"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ResourceSourceInfo {
	    id: string;
	    name: string;
	    siteUrl: string;
	    apiHost: string;
	    mirrorHost: string;
	    projectTypes: string[];
	    requiresApiKey: boolean;
	    apiKeyConfigured: boolean;
	    apiKeyApplyUrl: string;
	    available: boolean;
	    hint: string;
	
	    static createFrom(source: any = {}) {
	        return new ResourceSourceInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.siteUrl = source["siteUrl"];
	        this.apiHost = source["apiHost"];
	        this.mirrorHost = source["mirrorHost"];
	        this.projectTypes = source["projectTypes"];
	        this.requiresApiKey = source["requiresApiKey"];
	        this.apiKeyConfigured = source["apiKeyConfigured"];
	        this.apiKeyApplyUrl = source["apiKeyApplyUrl"];
	        this.available = source["available"];
	        this.hint = source["hint"];
	    }
	}
	export class ResourceVersion {
	    source: string;
	    projectId: string;
	    versionId: string;
	    name: string;
	    versionNumber: string;
	    changelog: string;
	    gameVersions: string[];
	    loaders: string[];
	    datePublished: string;
	    releaseType: string;
	    fileName: string;
	    fileUrl: string;
	    fileSize: number;
	    sha1: string;
	    downloadAllowed: boolean;
	    dependencies: ResourceDependency[];
	    displayName: string;
	    dateDisplay: string;
	    gameVersionsDisplay: string;
	    loaderDisplay: string;
	    summary: string;
	    fileSizeDisplay: string;
	    releaseTypeDisplay: string;
	    matchesInstance: boolean;
	    matchNote: string;
	
	    static createFrom(source: any = {}) {
	        return new ResourceVersion(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = source["source"];
	        this.projectId = source["projectId"];
	        this.versionId = source["versionId"];
	        this.name = source["name"];
	        this.versionNumber = source["versionNumber"];
	        this.changelog = source["changelog"];
	        this.gameVersions = source["gameVersions"];
	        this.loaders = source["loaders"];
	        this.datePublished = source["datePublished"];
	        this.releaseType = source["releaseType"];
	        this.fileName = source["fileName"];
	        this.fileUrl = source["fileUrl"];
	        this.fileSize = source["fileSize"];
	        this.sha1 = source["sha1"];
	        this.downloadAllowed = source["downloadAllowed"];
	        this.dependencies = this.convertValues(source["dependencies"], ResourceDependency);
	        this.displayName = source["displayName"];
	        this.dateDisplay = source["dateDisplay"];
	        this.gameVersionsDisplay = source["gameVersionsDisplay"];
	        this.loaderDisplay = source["loaderDisplay"];
	        this.summary = source["summary"];
	        this.fileSizeDisplay = source["fileSizeDisplay"];
	        this.releaseTypeDisplay = source["releaseTypeDisplay"];
	        this.matchesInstance = source["matchesInstance"];
	        this.matchNote = source["matchNote"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ResourceVersionList {
	    source: string;
	    projectId: string;
	    versions: ResourceVersion[];
	    matchedCount: number;
	    message: string;
	    needsApiKey: boolean;
	    usedMirror: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ResourceVersionList(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = source["source"];
	        this.projectId = source["projectId"];
	        this.versions = this.convertValues(source["versions"], ResourceVersion);
	        this.matchedCount = source["matchedCount"];
	        this.message = source["message"];
	        this.needsApiKey = source["needsApiKey"];
	        this.usedMirror = source["usedMirror"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ResourceVersionRequest {
	    source: string;
	    projectId: string;
	    gameVersion: string;
	    loader: string;
	
	    static createFrom(source: any = {}) {
	        return new ResourceVersionRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = source["source"];
	        this.projectId = source["projectId"];
	        this.gameVersion = source["gameVersion"];
	        this.loader = source["loader"];
	    }
	}

}

export namespace modpack {
	
	export class ModpackContentItem {
	    Category: string;
	    RelativePath: string;
	    Name: string;
	    SizeBytes: number;
	    IsDirectory: boolean;
	
	    static createFrom(source: any = {}) {
	        return new ModpackContentItem(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Category = source["Category"];
	        this.RelativePath = source["RelativePath"];
	        this.Name = source["Name"];
	        this.SizeBytes = source["SizeBytes"];
	        this.IsDirectory = source["IsDirectory"];
	    }
	}
	export class ModpackExportOptions {
	    Format: number;
	    PackName: string;
	    PackVersion: string;
	    Author: string;
	    UpdateLink: string;
	    Description: string;
	    IconPngPath: string;
	    MinecraftVersion: string;
	    LoaderName: string;
	    LoaderVersion: string;
	    IncludedPaths: string[];
	    ResolveModrinthLinks: boolean;
	    CurseForgeAPIKey: string;
	
	    static createFrom(source: any = {}) {
	        return new ModpackExportOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Format = source["Format"];
	        this.PackName = source["PackName"];
	        this.PackVersion = source["PackVersion"];
	        this.Author = source["Author"];
	        this.UpdateLink = source["UpdateLink"];
	        this.Description = source["Description"];
	        this.IconPngPath = source["IconPngPath"];
	        this.MinecraftVersion = source["MinecraftVersion"];
	        this.LoaderName = source["LoaderName"];
	        this.LoaderVersion = source["LoaderVersion"];
	        this.IncludedPaths = source["IncludedPaths"];
	        this.ResolveModrinthLinks = source["ResolveModrinthLinks"];
	        this.CurseForgeAPIKey = source["CurseForgeAPIKey"];
	    }
	}
	export class ModpackExportProfile {
	    packName?: string;
	    packVersion?: string;
	    author?: string;
	    description?: string;
	    updateLink?: string;
	    format?: number;
	    resolveModrinthLinks?: boolean;
	    excludedPaths?: string[];
	
	    static createFrom(source: any = {}) {
	        return new ModpackExportProfile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.packName = source["packName"];
	        this.packVersion = source["packVersion"];
	        this.author = source["author"];
	        this.description = source["description"];
	        this.updateLink = source["updateLink"];
	        this.format = source["format"];
	        this.resolveModrinthLinks = source["resolveModrinthLinks"];
	        this.excludedPaths = source["excludedPaths"];
	    }
	}
	export class ModpackExportResult {
	    OutputPath: string;
	    DeclaredFiles: number;
	    OverrideFiles: number;
	    Warnings: string[];
	
	    static createFrom(source: any = {}) {
	        return new ModpackExportResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.OutputPath = source["OutputPath"];
	        this.DeclaredFiles = source["DeclaredFiles"];
	        this.OverrideFiles = source["OverrideFiles"];
	        this.Warnings = source["Warnings"];
	    }
	}

}

export namespace monitoring {
	
	export class DiskUsage {
	    Path: string;
	    TotalBytes: number;
	    FreeBytes: number;
	    UsedPercent: number;
	
	    static createFrom(source: any = {}) {
	        return new DiskUsage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Path = source["Path"];
	        this.TotalBytes = source["TotalBytes"];
	        this.FreeBytes = source["FreeBytes"];
	        this.UsedPercent = source["UsedPercent"];
	    }
	}
	export class MemorySnapshot {
	    LauncherMemoryMb: number;
	    JvmMemoryMb: number;
	    JavaProcessCount: number;
	
	    static createFrom(source: any = {}) {
	        return new MemorySnapshot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.LauncherMemoryMb = source["LauncherMemoryMb"];
	        this.JvmMemoryMb = source["JvmMemoryMb"];
	        this.JavaProcessCount = source["JavaProcessCount"];
	    }
	}
	export class SystemUsage {
	    CpuPercent: number;
	    GpuPercent: number;
	    MemoryPercent: number;
	    MemoryUsedGb: number;
	    MemoryTotalGb: number;
	
	    static createFrom(source: any = {}) {
	        return new SystemUsage(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.CpuPercent = source["CpuPercent"];
	        this.GpuPercent = source["GpuPercent"];
	        this.MemoryPercent = source["MemoryPercent"];
	        this.MemoryUsedGb = source["MemoryUsedGb"];
	        this.MemoryTotalGb = source["MemoryTotalGb"];
	    }
	}

}

export namespace music {
	
	export class MusicTrack {
	    FilePath: string;
	    FileSize: number;
	    // Go type: time
	    LastModified: any;
	
	    static createFrom(source: any = {}) {
	        return new MusicTrack(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.FilePath = source["FilePath"];
	        this.FileSize = source["FileSize"];
	        this.LastModified = this.convertValues(source["LastModified"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace network {
	
	export class MinecraftServerStatus {
	    Motd: string;
	    VersionName: string;
	    ProtocolVersion: number;
	    OnlinePlayers: number;
	    MaxPlayers: number;
	    IconPath: string;
	
	    static createFrom(source: any = {}) {
	        return new MinecraftServerStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Motd = source["Motd"];
	        this.VersionName = source["VersionName"];
	        this.ProtocolVersion = source["ProtocolVersion"];
	        this.OnlinePlayers = source["OnlinePlayers"];
	        this.MaxPlayers = source["MaxPlayers"];
	        this.IconPath = source["IconPath"];
	    }
	}
	export class ProxySettings {
	    Mode: string;
	    Address: string;
	    Username: string;
	    Password: string;
	
	    static createFrom(source: any = {}) {
	        return new ProxySettings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Mode = source["Mode"];
	        this.Address = source["Address"];
	        this.Username = source["Username"];
	        this.Password = source["Password"];
	    }
	}
	export class ServerAddress {
	    Host: string;
	    Port: number;
	
	    static createFrom(source: any = {}) {
	        return new ServerAddress(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Host = source["Host"];
	        this.Port = source["Port"];
	    }
	}

}

export namespace online {
	
	export class HostOptions {
	    Provider: string;
	    Player: string;
	    Target: string;
	    ServerID: string;
	    MaxPlayers: number;
	
	    static createFrom(source: any = {}) {
	        return new HostOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Provider = source["Provider"];
	        this.Player = source["Player"];
	        this.Target = source["Target"];
	        this.ServerID = source["ServerID"];
	        this.MaxPlayers = source["MaxPlayers"];
	    }
	}
	export class LocalServer {
	    ID: string;
	    Name: string;
	    Port: number;
	    Status: string;
	    Running: boolean;
	
	    static createFrom(source: any = {}) {
	        return new LocalServer(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Name = source["Name"];
	        this.Port = source["Port"];
	        this.Status = source["Status"];
	        this.Running = source["Running"];
	    }
	}
	export class Player {
	    Name: string;
	    Kind: string;
	    Vendor: string;
	
	    static createFrom(source: any = {}) {
	        return new Player(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Name = source["Name"];
	        this.Kind = source["Kind"];
	        this.Vendor = source["Vendor"];
	    }
	}
	export class ProviderInfo {
	    ID: string;
	    Name: string;
	    Summary: string;
	    Homepage: string;
	    Ready: boolean;
	    Hint: string;
	    NeedsMod: boolean;
	    GuestNeedsMod: boolean;
	    HostNote: string;
	    JoinNote: string;
	
	    static createFrom(source: any = {}) {
	        return new ProviderInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ID = source["ID"];
	        this.Name = source["Name"];
	        this.Summary = source["Summary"];
	        this.Homepage = source["Homepage"];
	        this.Ready = source["Ready"];
	        this.Hint = source["Hint"];
	        this.NeedsMod = source["NeedsMod"];
	        this.GuestNeedsMod = source["GuestNeedsMod"];
	        this.HostNote = source["HostNote"];
	        this.JoinNote = source["JoinNote"];
	    }
	}
	export class RelayOption {
	    Name: string;
	    Address: string;
	    Builtin: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RelayOption(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Name = source["Name"];
	        this.Address = source["Address"];
	        this.Builtin = source["Builtin"];
	    }
	}
	export class RelayProbe {
	    Name: string;
	    Address: string;
	    Reachable: boolean;
	    LatencyMs: number;
	    Status: number;
	    Error: string;
	
	    static createFrom(source: any = {}) {
	        return new RelayProbe(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Name = source["Name"];
	        this.Address = source["Address"];
	        this.Reachable = source["Reachable"];
	        this.LatencyMs = source["LatencyMs"];
	        this.Status = source["Status"];
	        this.Error = source["Error"];
	    }
	}
	export class Runtime {
	    Provider: string;
	    Running: boolean;
	    Managed: boolean;
	    Version: string;
	    Binary: string;
	    Port: number;
	
	    static createFrom(source: any = {}) {
	        return new Runtime(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Provider = source["Provider"];
	        this.Running = source["Running"];
	        this.Managed = source["Managed"];
	        this.Version = source["Version"];
	        this.Binary = source["Binary"];
	        this.Port = source["Port"];
	    }
	}
	export class Settings {
	    Provider: string;
	    Player: string;
	    TerracottaPath: string;
	    RedstoneRelay: string;
	    RedstoneKey: string;
	    Target: string;
	    ServerID: string;
	    MaxPlayers: number;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Provider = source["Provider"];
	        this.Player = source["Player"];
	        this.TerracottaPath = source["TerracottaPath"];
	        this.RedstoneRelay = source["RedstoneRelay"];
	        this.RedstoneKey = source["RedstoneKey"];
	        this.Target = source["Target"];
	        this.ServerID = source["ServerID"];
	        this.MaxPlayers = source["MaxPlayers"];
	    }
	}
	export class Status {
	    Provider: string;
	    State: string;
	    Phase: string;
	    Room: string;
	    Address: string;
	    LocalAddress: string;
	    JoinHost: string;
	    JoinPort: number;
	    Players: Player[];
	    Connections: number;
	    Tip: string;
	    RelayNote: string;
	    Error: string;
	    Since: number;
	
	    static createFrom(source: any = {}) {
	        return new Status(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Provider = source["Provider"];
	        this.State = source["State"];
	        this.Phase = source["Phase"];
	        this.Room = source["Room"];
	        this.Address = source["Address"];
	        this.LocalAddress = source["LocalAddress"];
	        this.JoinHost = source["JoinHost"];
	        this.JoinPort = source["JoinPort"];
	        this.Players = this.convertValues(source["Players"], Player);
	        this.Connections = source["Connections"];
	        this.Tip = source["Tip"];
	        this.RelayNote = source["RelayNote"];
	        this.Error = source["Error"];
	        this.Since = source["Since"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TerracottaInstallResult {
	    Version: string;
	    AssetName: string;
	    Path: string;
	    ManualHint: string;
	
	    static createFrom(source: any = {}) {
	        return new TerracottaInstallResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Version = source["Version"];
	        this.AssetName = source["AssetName"];
	        this.Path = source["Path"];
	        this.ManualHint = source["ManualHint"];
	    }
	}

}

export namespace solo {
	
	export class SoloExportOptions {
	    PackName: string;
	    PackVersion: string;
	    Author: string;
	    UpdateLink: string;
	    Description: string;
	    IconPngPath: string;
	    MinecraftVersion: string;
	    LoaderName: string;
	    LoaderVersion: string;
	    IncludedPaths: string[];
	    ContentDirectory: string;
	    VersionDirectory: string;
	    VersionID: string;
	    BundleJava: boolean;
	    SimpleMode: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SoloExportOptions(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.PackName = source["PackName"];
	        this.PackVersion = source["PackVersion"];
	        this.Author = source["Author"];
	        this.UpdateLink = source["UpdateLink"];
	        this.Description = source["Description"];
	        this.IconPngPath = source["IconPngPath"];
	        this.MinecraftVersion = source["MinecraftVersion"];
	        this.LoaderName = source["LoaderName"];
	        this.LoaderVersion = source["LoaderVersion"];
	        this.IncludedPaths = source["IncludedPaths"];
	        this.ContentDirectory = source["ContentDirectory"];
	        this.VersionDirectory = source["VersionDirectory"];
	        this.VersionID = source["VersionID"];
	        this.BundleJava = source["BundleJava"];
	        this.SimpleMode = source["SimpleMode"];
	    }
	}
	export class StubStatus {
	    Found: boolean;
	    Path: string;
	
	    static createFrom(source: any = {}) {
	        return new StubStatus(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Found = source["Found"];
	        this.Path = source["Path"];
	    }
	}

}

export namespace update {
	
	export class Asset {
	    Name: string;
	    URL: string;
	    Size: number;
	
	    static createFrom(source: any = {}) {
	        return new Asset(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Name = source["Name"];
	        this.URL = source["URL"];
	        this.Size = source["Size"];
	    }
	}
	export class CheckResult {
	    CurrentVersion: string;
	    LatestVersion: string;
	    UpdateAvailable: boolean;
	    Prerelease: boolean;
	    Notes: string;
	    PublishedAt: number;
	    Asset?: Asset;
	    PageURL: string;
	    ManualHint: string;
	    CanSelfUpdate: boolean;
	
	    static createFrom(source: any = {}) {
	        return new CheckResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.CurrentVersion = source["CurrentVersion"];
	        this.LatestVersion = source["LatestVersion"];
	        this.UpdateAvailable = source["UpdateAvailable"];
	        this.Prerelease = source["Prerelease"];
	        this.Notes = source["Notes"];
	        this.PublishedAt = source["PublishedAt"];
	        this.Asset = this.convertValues(source["Asset"], Asset);
	        this.PageURL = source["PageURL"];
	        this.ManualHint = source["ManualHint"];
	        this.CanSelfUpdate = source["CanSelfUpdate"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace world {
	
	export class WorldInfo {
	    Name: string;
	    DirectoryPath: string;
	    OwnerVersionId: string;
	    // Go type: time
	    LastPlayed: any;
	    IconPath: string;
	
	    static createFrom(source: any = {}) {
	        return new WorldInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Name = source["Name"];
	        this.DirectoryPath = source["DirectoryPath"];
	        this.OwnerVersionId = source["OwnerVersionId"];
	        this.LastPlayed = this.convertValues(source["LastPlayed"], null);
	        this.IconPath = source["IconPath"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}


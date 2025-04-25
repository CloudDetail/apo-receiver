package ebpffile

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/CloudDetail/apo-receiver/pkg/httphelper"
	"github.com/CloudDetail/apo-receiver/pkg/model"
)

type EbpfFIleServer struct {
	model.UnimplementedFileServiceServer
	centerServerAddr string
	portalAddress    string
}

type EbpfFileRep struct {
	FileName    string `json:"fileName"`
	FileContent []byte `json:"fileContent"`
}

func NewEbpfFIleServer(centerServerAddr string, portalAddress string) *EbpfFIleServer {
	return &EbpfFIleServer{
		centerServerAddr: centerServerAddr,
		portalAddress:    portalAddress,
	}
}

func (server *EbpfFIleServer) GetFile(ctx context.Context, request *model.FileRequest) (*model.FileResponse, error) {
	log.Println(request.AgentVersion, request.OsVersion, request.KernelVersion, request.OsDistribution, request.Arch)
	fileResp := &model.FileResponse{}
	// checkFetchVersion
	version := checkVersion(request.OsDistribution, request.Arch)
	filePath, err := checkLocalEbpfFile(request, version)
	log.Println("get ebpf file:", filePath)
	if os.IsNotExist(err) {
		log.Println("fetch ebpf file from center server")
		ebpfFileRep := getEbpfFileFromCenter(server.portalAddress, server.centerServerAddr, request, version)
		if len(ebpfFileRep.FileContent) != 0 {
			var fp string
			if version == EBPF_FETCH_API_V1 {
				fp = "/opt/" + request.AgentVersion + "/" + request.OsDistribution + "/" + request.Arch + "/"
			} else {
				fp = "/opt/" + request.AgentVersion + "/" + request.OsVersion + "/"
			}

			err := os.MkdirAll(filepath.Dir(fp), 0755)
			if err != nil {
				return nil, err
			}
			decodedBytes, _ := base64.StdEncoding.DecodeString(ebpfFileRep.FileContent)
			err = os.WriteFile(filePath, decodedBytes, 0644)
			if err != nil {
				return nil, err
			}
			log.Println("new ebpfFile is download: " + filePath)
			fileResp.FileContent = decodedBytes
			fileResp.FileName = request.KernelVersion + ".o"
		} else {
			log.Println("no such ebpfFile is founded: " + filePath)
		}
	} else {
		fileContent, err := os.ReadFile(filePath)
		if err != nil {
			return nil, err
		}
		fileResp.FileContent = fileContent
		fileResp.FileName = request.KernelVersion + ".o"
	}
	return fileResp, nil
}

type Response struct {
	FileName    string `json:"fileName"`
	FileContent string `json:"fileContent"`
}

func getEbpfFileFromCenter(portalAddress string, centerServerAddr string, req *model.FileRequest, version EbpfFetchAPI) Response {
	ebpfFileRep := getCenterEbpfFile(portalAddress, centerServerAddr, req, version)
	if len(ebpfFileRep.FileContent) == 0 && version == EBPF_FETCH_API_V1 {
		ebpfFileRep = getCenterEbpfFile(portalAddress, centerServerAddr, req, EBPF_FETCH_API)
	}
	return ebpfFileRep
}

func getCenterEbpfFile(portalAddress string, centerServerAddr string, req *model.FileRequest, version EbpfFetchAPI) Response {
	var response Response
	resp, err := queryEbpfFile(portalAddress, centerServerAddr, req, version)
	if err != nil {
		log.Printf("get ebpf dile from server[%s] error: %s", centerServerAddr, err)
		return response
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[get ebpf file] failed to read the response body: %v", err)
		return response
	}
	if err = json.Unmarshal(body, &response); err != nil {
		log.Printf("[get ebpf file] failed to decode SLOAlias Response error: %v", err)
		return response
	}

	return response

}

func queryEbpfFile(portalAddress string, centerServerAddr string, req *model.FileRequest, version EbpfFetchAPI) (*http.Response, error) {
	client := httphelper.CreateHttpClient(portalAddress != "", portalAddress)
	var reqUrl string
	if version == EBPF_FETCH_API_V1 {
		reqUrl = fmt.Sprintf("http://%s/ebpffile/v1/download?agentVersion=%s&osDistribution=%s&arch=%s&kernelVersion=%s", centerServerAddr, req.AgentVersion, req.OsDistribution, req.Arch, req.KernelVersion)
	} else {
		reqUrl = fmt.Sprintf("http://%s/ebpffile/download?agentVersion=%s&osVersion=%s&kernelVersion=%s", centerServerAddr, req.AgentVersion, req.OsVersion, req.KernelVersion)
	}

	httpReq, err := http.NewRequest("GET", reqUrl, nil)
	if err != nil {
		return nil, err
	}
	return client.Do(httpReq)
}
